package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExampleShape(t *testing.T) {
	yaml := `
server:
  listen: "127.0.0.1:18080"
policy:
  dry_run: true
  require_confirm_for_writes: true
  audit_db: "test-audit.jsonl"
platforms:
  feishu:
    enabled: true
    app_id: "APP"
    app_secret: "SECRET"
    domain: "https://open.feishu.cn"
  dingtalk:
    enabled: false
    app_key: "KEY"
allowlist:
  users:
    - alice
    - bob
  groups: []
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:18080" || !cfg.DryRun || !cfg.RequireConfirm {
		t.Fatalf("bad server/policy: %+v", cfg)
	}
	if cfg.Platforms["feishu"].AppID != "APP" || !cfg.Platforms["feishu"].Enabled {
		t.Fatalf("bad feishu platform: %+v", cfg.Platforms["feishu"])
	}
	if len(cfg.AllowUsers) != 2 || cfg.AllowUsers[0] != "alice" {
		t.Fatalf("bad allowlist: %v", cfg.AllowUsers)
	}
}

func TestLoadDefaultsAreSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: \"127.0.0.1:18081\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DryRun || !cfg.RequireConfirm {
		t.Fatalf("defaults must be safe, got dry_run=%v require_confirm=%v", cfg.DryRun, cfg.RequireConfirm)
	}
	if cfg.AuditDB == "" {
		t.Fatal("audit_db default must not be empty")
	}
}
