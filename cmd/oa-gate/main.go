// Command oa-gate runs the unified office agent gateway.
//
// Usage:
//
//	oa-gate -config config.yaml            # HTTP JSON API (default)
//	oa-gate -config config.yaml -transport stdio  # MCP over stdio
//
// Copy config.example.yaml to config.yaml first and fill in your own
// self-built app credentials. Never commit config.yaml.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/SoftenTheGlare382/oa-gate/internal/config"
	"github.com/SoftenTheGlare382/oa-gate/internal/gateway"
	"github.com/SoftenTheGlare382/oa-gate/internal/server"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	transport := flag.String("transport", "http", "transport: http or stdio (MCP)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config %s: %v", *cfgPath, err)
	}
	g, err := gateway.New(cfg)
	if err != nil {
		log.Fatalf("build gateway: %v", err)
	}
	ctx := context.Background()
	switch *transport {
	case "stdio":
		fmt.Fprintln(os.Stderr, "oa-gate: MCP stdio transport, dry_run="+boolStr(cfg.DryRun))
		if err := server.ServeMCP(ctx, g, os.Stdin, os.Stdout); err != nil {
			log.Fatalf("mcp: %v", err)
		}
	case "http":
		fmt.Printf("oa-gate: listening on %s (dry_run=%v)\n", cfg.Listen, cfg.DryRun)
		if err := http.ListenAndServe(cfg.Listen, server.NewHTTPHandler(g)); err != nil {
			log.Fatalf("http: %v", err)
		}
	default:
		log.Fatalf("unknown transport %q (want http or stdio)", *transport)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
