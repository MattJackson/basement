// Command basement-mcp is the Model Context Protocol server for
// basement. It exposes a curated subset of basement-server's API as
// MCP tools so AI agents (Claude Code, Claude Desktop, Cursor) can
// drive storage workflows via natural language: "find PDFs in the
// 'lsi' region's 'broadcom' bucket and back them up to b2".
//
// The binary is a *stdio* MCP server, NOT an HTTP server. Claude
// spawns basement-mcp as a subprocess; we read JSON-RPC 2.0
// messages off stdin one line at a time and write responses to
// stdout. The host process (Claude) routes them to the active
// conversation. Logs and diagnostics MUST go to stderr — anything
// on stdout that isn't a framed RPC response will scramble the
// transport.
//
// Auth: same as cmd/basement (the v1.8.0a CLI). We read
// ~/.config/basement/config.yaml via internal/clilib, resolve a
// profile, and call basement-server's API with
// "Authorization: Bearer AKID:SECRET" per the v1.7.0b service-
// account middleware (internal/auth/bearer.go).
//
// Protocol version: 2024-11-05 — the long-stable baseline shipped
// with the original MCP spec. We don't yet advertise the newer
// 2025-03-26 streaming-HTTP variant because (a) we run stdio-only
// and (b) Claude Code clients have been backward-compatible across
// the two for the entire v1.7→v1.8 development window.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/mattjackson/basement/internal/clilib"
	"github.com/mattjackson/basement/internal/version"
)

func main() {
	var profileFlag string
	var versionFlag bool
	flag.StringVar(&profileFlag, "profile", "", "basement profile name (defaults to $BASEMENT_PROFILE or 'default')")
	flag.BoolVar(&versionFlag, "version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: basement-mcp [--profile NAME]\n\n")
		fmt.Fprintf(os.Stderr, "basement-mcp is a Model Context Protocol stdio server that exposes\n")
		fmt.Fprintf(os.Stderr, "basement storage operations as tools for AI agents (Claude, Cursor).\n\n")
		fmt.Fprintf(os.Stderr, "Spawn this binary from your MCP-aware client; it reads JSON-RPC\n")
		fmt.Fprintf(os.Stderr, "off stdin and writes responses to stdout. Auth is read from\n")
		fmt.Fprintf(os.Stderr, "~/.config/basement/config.yaml.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if versionFlag {
		info := version.Get()
		fmt.Printf("basement-mcp %s (%s, built %s)\n", info.Version, info.Commit, info.BuiltAt)
		return
	}

	// All logs to stderr — stdout is reserved for JSON-RPC frames.
	// JSON handler keeps things machine-parseable if the host
	// captures the stream (Claude Code surfaces stderr in its MCP
	// inspector panel).
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Resolve the profile up-front so a misconfigured deployment
	// fails the initialize handshake cleanly (clients show a tool-
	// loading error) instead of failing every tool-call later.
	cfg, err := clilib.LoadConfig()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	name := clilib.ProfileName(profileFlag)
	profile, err := clilib.ResolveProfile(cfg, name)
	if err != nil {
		logger.Error("resolve profile",
			"profile", name,
			"error", err,
			"hint", "create ~/.config/basement/config.yaml with the active service-account creds, or set $BASEMENT_PROFILE")
		os.Exit(1)
	}

	client := clilib.NewClient(profile.Endpoint, profile.AccessKeyID, profile.SecretKey)
	logger.Info("basement-mcp starting",
		"version", version.Get().Version,
		"profile", name,
		"endpoint", client.Endpoint())

	srv := NewServer(client, logger)
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		logger.Error("serve", "error", err)
		os.Exit(1)
	}
}
