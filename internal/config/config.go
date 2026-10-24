// Package config loads oa-gate configuration from a small YAML subset.
//
// The subset is deliberately limited to what config.example.yaml uses:
// nested maps (2-space indent), scalar values, empty lists ([]) and
// dash lists. Keeping the parser in stdlib means `go build` works
// without a module proxy or network access.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// PlatformConfig holds one platform's credentials and test knobs.
// BaseURL is only for tests (httptest servers); leave it empty in
// production so the platform default is used. UserID / ProcessCode /
// CalendarID are optional platform-specific knobs, documented in
// config.example.yaml.
type PlatformConfig struct {
	Enabled     bool
	AppID       string
	AppSecret   string
	AppKey      string
	AgentID     string
	CorpID      string
	Secret      string
	Domain      string
	BaseURL     string
	UserID      string
	ProcessCode string
	CalendarID  string
}

// Config is the whole gateway configuration.
type Config struct {
	Listen         string
	DryRun         bool
	RequireConfirm bool
	AuditDB        string
	Platforms      map[string]PlatformConfig
	AllowUsers     []string
	AllowGroups    []string
}

// Default returns a safe configuration: dry-run on, confirmation required.
func Default() Config {
	return Config{
		Listen:         "127.0.0.1:8080",
		DryRun:         true,
		RequireConfirm: true,
		AuditDB:        "oa-gate-audit.jsonl",
		Platforms:      map[string]PlatformConfig{},
	}
}

// Load reads and parses path, applying safe defaults for missing keys.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	root, err := parseYAML(string(data))
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if m, ok := root["server"].(map[string]any); ok {
		if v, ok := m["listen"].(string); ok && v != "" {
			cfg.Listen = v
		}
	}
	if m, ok := root["policy"].(map[string]any); ok {
		if v, ok := m["dry_run"].(bool); ok {
			cfg.DryRun = v
		}
		if v, ok := m["require_confirm_for_writes"].(bool); ok {
			cfg.RequireConfirm = v
		}
		if v, ok := m["audit_db"].(string); ok && v != "" {
			cfg.AuditDB = v
		}
	}
	if m, ok := root["platforms"].(map[string]any); ok {
		for name, raw := range m {
			pm, _ := raw.(map[string]any)
			if pm == nil {
				continue
			}
			p := PlatformConfig{
				Enabled:     boolVal(pm, "enabled"),
				AppID:       strVal(pm, "app_id"),
				AppSecret:   strVal(pm, "app_secret"),
				AppKey:      strVal(pm, "app_key"),
				AgentID:     strVal(pm, "agent_id"),
				CorpID:      strVal(pm, "corp_id"),
				Secret:      strVal(pm, "secret"),
				Domain:      strVal(pm, "domain"),
				BaseURL:     strVal(pm, "base_url"),
				UserID:      strVal(pm, "user_id"),
				ProcessCode: strVal(pm, "process_code"),
				CalendarID:  strVal(pm, "calendar_id"),
			}
			cfg.Platforms[name] = p
		}
	}
	if m, ok := root["allowlist"].(map[string]any); ok {
		cfg.AllowUsers = strList(m, "users")
		cfg.AllowGroups = strList(m, "groups")
	}
	return cfg, nil
}

func strVal(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func boolVal(m map[string]any, k string) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return false
}

func strList(m map[string]any, k string) []string {
	switch v := m[k].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// --- minimal YAML subset parser ---

type frame struct {
	indent int
	m      map[string]any
	key    string // for a list frame: key in m that holds the list
	isList bool
}

func cleanLine(raw string) (indent int, content string, ok bool) {
	if idx := strings.Index(raw, " #"); idx >= 0 {
		raw = raw[:idx]
	}
	trimmed := strings.TrimRight(raw, " \t\r")
	if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
		return 0, "", false
	}
	indent = len(trimmed) - len(strings.TrimLeft(trimmed, " "))
	return indent, strings.TrimSpace(trimmed), true
}

func parseYAML(input string) (map[string]any, error) {
	root := map[string]any{}
	stack := []frame{{indent: -1, m: root}}
	lines := strings.Split(input, "\n")
	for i, raw := range lines {
		indent, content, ok := cleanLine(raw)
		if !ok {
			continue
		}
		for len(stack) > 1 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		top := stack[len(stack)-1]
		if strings.HasPrefix(content, "- ") || content == "-" {
			if !top.isList {
				return nil, fmt.Errorf("line %d: list item without a list key", i+1)
			}
			item := strings.TrimSpace(strings.TrimPrefix(content, "-"))
			lst, _ := top.m[top.key].([]any)
			lst = append(lst, parseScalar(item))
			top.m[top.key] = lst
			continue
		}
		cur := top.m
		idx := strings.Index(content, ":")
		if idx < 0 {
			return nil, fmt.Errorf("line %d: expected key: value", i+1)
		}
		key := strings.TrimSpace(content[:idx])
		val := strings.TrimSpace(content[idx+1:])
		if val == "" {
			// Look ahead: dash items mean a list, otherwise a nested map.
			isList := false
			for j := i + 1; j < len(lines); j++ {
				_, next, ok := cleanLine(lines[j])
				if !ok {
					continue
				}
				isList = strings.HasPrefix(next, "- ") || next == "-"
				break
			}
			if isList {
				cur[key] = []any{}
				stack = append(stack, frame{indent: indent, m: cur, key: key, isList: true})
			} else {
				child := map[string]any{}
				cur[key] = child
				stack = append(stack, frame{indent: indent, m: child})
			}
			continue
		}
		if val == "[]" {
			cur[key] = []any{}
			continue
		}
		cur[key] = parseScalar(val)
	}
	return root, nil
}

func parseScalar(s string) any {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null", "~":
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return strconv.Itoa(n)
	}
	return s
}
