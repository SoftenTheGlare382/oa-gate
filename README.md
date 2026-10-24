# oa-gate

Go-native, self-hosted agent gateway for China's office ecosystems: one unified tool layer across **Feishu, DingTalk, and WeCom (企业微信)**.

Focus: **approvals first, not just chat bots.** Messages, approvals, calendars and documents are exposed to agents under the same tool names, with safety built in by default.

> Status: v0.1.0. The gateway runs over HTTP JSON and MCP stdio, with dry-run, confirmation, idempotency and audit logging enforced in the gateway layer — not left to each caller. Live platform calls have **not** been tested against a real tenant (no tenant credentials were provided); adapters are tested against mock platform servers, and the message/approval paths follow the platforms' public HTTP APIs. See [Platform status](#platform-status) before going live.

## Why this exists

- Single-platform MCP servers already exist (e.g. the official Feishu/Lark MCP server), and generic agent frameworks already offer multi-platform *message channels*.
- What is missing is a **Go, single-binary, approval-first unified OA tool layer**: the same `office.approval.list / get / decide` across Feishu, DingTalk and WeCom, with dry-run, explicit confirmation, idempotency keys and an audit log.
- This project does **not** claim to be the first multi-platform gateway. It claims a narrower position: the first Go-native, approval-first unified OA tool layer for these three platforms.

## Safety defaults (non-negotiable)

- `policy.dry_run` defaults to `true`: write and approval actions are simulated until explicitly enabled. A dry-run never calls a platform adapter.
- `office.approval.decide` requires `confirm=true` **and** an `idempotency_key`. A repeated key returns the stored result and never executes twice.
- Failed decisions do **not** consume the idempotency key, so the caller can fix the cause and retry with the same key.
- Every approval decision (including dry-run previews) is appended to the audit log (`oa-gate-audit.jsonl` by default, append-only JSON lines).
- Platform requests retry only transport errors and 429/503 responses, never a response the platform already answered.

## Quick start

```bash
cp config.example.yaml config.yaml
# Fill in your own self-built app credentials in config.yaml (never commit it)
go run ./cmd/oa-gate
# or: go build -o oa-gate ./cmd/oa-gate && ./oa-gate -config config.yaml
```

Health check and tools:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/tools
```

Send a message (dry-run preview while `dry_run: true`):

```bash
curl -X POST http://127.0.0.1:8080/tools/office.message.send \
  -H 'Content-Type: application/json' \
  -d '{"platform":"feishu","target":"USER_ID","text":"hello from oa-gate"}'
```

Decide an approval (real execution only with `dry_run: false` in config):

```bash
curl -X POST http://127.0.0.1:8080/tools/office.approval.decide \
  -H 'Content-Type: application/json' \
  -d '{"platform":"feishu","approval_id":"TASK_ID","decision":"approve","confirm":true,"idempotency_key":"approve-2026-001"}'
```

### MCP (stdio)

```bash
./oa-gate -config config.yaml -transport stdio
```

The MCP server speaks JSON-RPC 2.0 over stdio and supports `initialize`, `ping`, `tools/list` and `tools/call` with the same tool names as the HTTP API, so policy is identical on both transports. Example client config (Claude/Cursor style):

```json
{
  "mcpServers": {
    "oa-gate": {
      "command": "/path/to/oa-gate",
      "args": ["-config", "/path/to/config.yaml", "-transport", "stdio"]
    }
  }
}
```

## Unified tools

See [docs/unified-tools.md](docs/unified-tools.md) for the full list and parameters.

- `office.message.send`
- `office.approval.list` / `office.approval.get` / `office.approval.decide`
- `office.calendar.list` (read-only)
- `office.doc.search` / `office.doc.read` (read-only)

## Platform status (v0.1, honest table)

| Tool | Feishu | DingTalk | WeCom |
|---|---|---|---|
| message.send | ✅ implemented | ✅ implemented | ✅ implemented |
| approval.list / get | ✅ implemented (needs `user_id`) | ✅ read, needs `process_code` | ✅ read |
| approval.decide | ✅ implemented (needs `user_id`) | ❌ refused with a clear error — task-approval API varies by process type, so oa-gate does not guess an endpoint | ❌ refused with a clear error, same reason |
| calendar.list | ✅ implemented (needs `calendar_id`) | ❌ not implemented | ❌ not implemented |
| doc.search / read | ✅ implemented | ❌ not implemented | ❌ not implemented |

A refused decide is a safety feature, not a bug: it is never disguised as a success, and it does not consume the idempotency key.

## Development and systematic checks

Stdlib only — no external Go dependencies, so builds work offline:

```bash
go vet ./...
go test ./...
go build -o oa-gate ./cmd/oa-gate
```

The test suite covers: config parsing and safe defaults, dry-run never calling an adapter, decide requiring confirmation and an idempotency key, idempotent replay, approval sorting, mock-server message sends for all three platforms, HTTP decide validation, and MCP `tools/list` + `tools/call`.

## Explicitly out of scope for v0.1

- Online document editing, workflow designers, enterprise SSO / multi-tenancy
- Personal WeChat (only WeCom), UI automation / simulated clicks
- Any third-party relay cloud service: this gateway is self-hosted

## License

MIT
