package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/uniroute/unirouter/internal/contentcapture"
)

const contentStoreUsage = "usage: uniroute content-store <init|check|export-token|reclaim-unpublished> --root <absolute-directory> --identity <identity> --token-file <absolute-file>"

func runContentStoreCLI(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 && args[0] == "reclaim-unpublished" {
		return runContentReclaimCLI(ctx, args[1:], output)
	}
	if len(args) == 0 || (args[0] != "init" && args[0] != "check" && args[0] != "export-token") {
		return errors.New(contentStoreUsage)
	}
	fs := flag.NewFlagSet("content-store", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", "", "existing private storage directory")
	identity := fs.String("identity", "", "exact storage identity")
	token := fs.String("token-file", "", "private binding file path")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *root == "" || *identity == "" || *token == "" {
		return errors.New(contentStoreUsage)
	}
	if ctx == nil {
		return errors.New("content-store requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	switch args[0] {
	case "init":
		err = contentcapture.InitializeBoundFileStore(ctx, *root, *identity, *token)
	case "export-token":
		err = contentcapture.ExportFileStoreToken(ctx, *root, *identity, *token)
	case "check":
		var store *contentcapture.FileStore
		store, err = contentcapture.OpenBoundFileStore(*root, *identity, *token)
		if err == nil {
			err = errors.Join(store.CheckReady(ctx), store.Close())
		}
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]string{"action": args[0], "status": "ok", "identity": *identity, "root": *root, "token_file": *token})
}
