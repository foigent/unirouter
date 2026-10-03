package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/uniroute/unirouter/internal/config"
)

const xInsightPeerUsage = "usage: uniroute xinsight-peer prepare --xi-origin <https-origin> --xi-deployment-id <id> --ur-origin <https-origin> --receiver-ip <host-cidr> --out <new-absolute-env-file> | check --file <absolute-env-file>"

// This installer entry prepares public static trust only. It does not enable
// the agent/capture, contact a peer, edit a shared .env, or load credentials.
func runXInsightPeerCLI(args []string, output io.Writer) error {
	if len(args) == 0 || (args[0] != "prepare" && args[0] != "check") {
		return errors.New(xInsightPeerUsage)
	}
	fs := flag.NewFlagSet("xinsight-peer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	xiOrigin := fs.String("xi-origin", "", "installed XI HTTPS control origin")
	deployment := fs.String("xi-deployment-id", "", "installed XI deployment identity")
	urOrigin := fs.String("ur-origin", "", "formal UR HTTPS origin")
	ip := fs.String("receiver-ip", "", "operator-verified receiver /32 or /128")
	out := fs.String("out", "", "new public trust environment snippet")
	file := fs.String("file", "", "existing trust snippet")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		return errors.New(xInsightPeerUsage)
	}
	var peer config.XInsightConnectorStaticPeer
	path := *file
	if args[0] == "prepare" {
		path = *out
	}
	if path != "" {
		parent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil || parent != filepath.Dir(path) {
			return fmt.Errorf("static peer file requires an existing non-aliased parent")
		}
	}
	if args[0] == "prepare" {
		if *file != "" || *out == "" || !filepath.IsAbs(*out) || filepath.Clean(*out) != *out {
			return errors.New(xInsightPeerUsage)
		}
		digest, err := config.XInsightManagedFeedNetworkDigest(*urOrigin)
		if err != nil {
			return err
		}
		peer = config.XInsightConnectorStaticPeer{XInsightOrigin: *xiOrigin, XDeploymentID: *deployment,
			UROrigin: *urOrigin, AllowedIP: *ip, NetworkBindingSHA256: digest}
		if err := peer.Validate(); err != nil {
			return err
		}
		wire, err := xInsightPeerEnvironment(peer)
		if err != nil {
			return err
		}
		if err := writeFreshFile(*out, wire); err != nil {
			return err
		}
		*file = *out
	} else if *file == "" || !filepath.IsAbs(*file) || filepath.Clean(*file) != *file ||
		*out != "" || *xiOrigin != "" || *deployment != "" || *urOrigin != "" || *ip != "" {
		return errors.New(xInsightPeerUsage)
	}
	wire, err := readFreshFile(*file, 4096)
	if err != nil {
		return err
	}
	lines := strings.Split(string(wire), "\n")
	if len(lines) != 3 || lines[2] != "" || !strings.HasPrefix(lines[0], "XINSIGHT_CONNECTOR_CONTROL_ORIGIN=") ||
		!strings.HasPrefix(lines[1], "XINSIGHT_CONNECTOR_STATIC_PEER_JSON='") || !strings.HasSuffix(lines[1], "'") {
		return fmt.Errorf("invalid static peer environment snippet")
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(lines[1], "XINSIGHT_CONNECTOR_STATIC_PEER_JSON='"), "'")
	if json.Unmarshal([]byte(raw), &peer) != nil || peer.Validate() != nil {
		return fmt.Errorf("invalid static peer readback")
	}
	expected, err := xInsightPeerEnvironment(peer)
	if err != nil || string(expected) != string(wire) {
		return fmt.Errorf("static peer environment readback differs")
	}
	return json.NewEncoder(output).Encode(map[string]any{"action": args[0], "status": "verified",
		"file": *file, "peer": peer, "agent_changed": false, "database_writes": 0})
}

func xInsightPeerEnvironment(peer config.XInsightConnectorStaticPeer) ([]byte, error) {
	if strings.ContainsAny(peer.XInsightOrigin+peer.UROrigin, "'\"\\$`") {
		return nil, fmt.Errorf("unsupported characters in static peer origin")
	}
	wire, err := json.Marshal(peer)
	if err != nil {
		return nil, err
	}
	return []byte("XINSIGHT_CONNECTOR_CONTROL_ORIGIN=" + peer.XInsightOrigin +
		"\nXINSIGHT_CONNECTOR_STATIC_PEER_JSON='" + string(wire) + "'\n"), nil
}
