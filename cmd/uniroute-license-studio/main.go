package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/uniroute/unirouter/internal/licensestudio"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "License Studio 启动失败：", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("uniroute-license-studio", flag.ContinueOnError)
	listenAddress := flags.String("listen", "127.0.0.1:0", "local listen address")
	noOpen := flags.Bool("no-open", false, "do not open the default browser")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", *listenAddress)
	if err != nil {
		return fmt.Errorf("listen on local interface: %w", err)
	}
	defer listener.Close()
	host := listener.Addr().String()
	if parsedHost, _, err := net.SplitHostPort(host); err != nil || !net.ParseIP(parsedHost).IsLoopback() {
		return fmt.Errorf("License Studio must listen on a loopback address")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}

	var httpServer *http.Server
	idleTimer := time.AfterFunc(2*time.Hour, func() {
		if httpServer != nil {
			_ = httpServer.Shutdown(context.Background())
		}
	})
	defer idleTimer.Stop()
	resetIdle := func() { idleTimer.Reset(2 * time.Hour) }
	shutdown := func() {
		if httpServer == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}
	handler, err := licensestudio.NewServer(licensestudio.ServerOptions{
		Dialogs:      licensestudio.NewNativeFileDialogs(),
		SessionToken: token,
		AllowedHost:  host,
		OnActivity:   resetIdle,
		Shutdown:     shutdown,
	})
	if err != nil {
		return err
	}
	httpServer = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	url := "http://" + host + "/" + token + "/"
	if *noOpen {
		// Developer/browser-test mode needs the ephemeral URL. Normal app
		// launches do not print the session token to stdout or system logs.
		fmt.Println(url)
	} else {
		if err := openBrowser(url); err != nil {
			return err
		}
	}

	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopSignals)
	go func() {
		<-stopSignals
		shutdown()
	}()
	serveErr := httpServer.Serve(listener)
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return fmt.Errorf("serve License Studio: %w", serveErr)
	}
	return nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate local session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("open License Studio in browser: %w", err)
	}
	return nil
}
