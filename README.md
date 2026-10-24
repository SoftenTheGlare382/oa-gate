# oa-gate

Go-native, self-hosted agent gateway for China's office ecosystems: one unified tool layer across **Feishu, DingTalk, and WeCom (企业微信)**.

Focus: **approvals first, not just chat bots.** Messages, approvals, calendars and documents are exposed to agents under the same tool names, with safety built in by default.

> Status: v0.0.0 skeleton. Phase 1 (Feishu message + approval read, approval decide with confirmation) is under development. No real credentials are stored in this repo — see `config.example.yaml`.

## Why this exists

- Single-platform MCP servers already exist (e.g. the official Feishu/Lark MCP server), and generic agent frameworks already offer multi-platform *message channels*.
- What is missing is a **Go, single-binary, approval-first unified OA tool layer**: the same `office.approval.list / get / decide` across Feishu, DingTalk and WeCom, with dry-run, explicit confirmation, idempotency keys and an audit log.
- This project does **not** claim to be the first multi-platform gateway. It claims a narrower position: the first Go-native, approval-first unified OA tool layer for these three platforms.

## Safety defaults (non-negotiable)

- `policy.dry_run` defaults to `true`: write and approval actions are simulated until explicitly enabled.
- `office.approval.decide` requires `confirm=true` **and** an `idempotency_key`. A repeated idempotency key must never execute twice.
- Every approval decision is written to the audit log: who, which approval, what decision, when, idempotency key, platform receipt.
- Platform rate limits are respected with backoff. Failures never silently retry an approval.

## Quick start (planned)

```bash
cp config.example.yaml config.yaml
# Fill in your own self-built app credentials in config.yaml (never commit it)
go run ./cmd/oa-gate
```

## Unified tools

See [docs/unified-tools.md](docs/unified-tools.md) for the full tool list and parameters.

Phase 1:

- `office.message.send`
- `office.approval.list`
- `office.approval.get`
- `office.approval.decide`

Phase 2 (read-only first):

- `office.calendar.list`
- `office.doc.search` / `office.doc.read`

## Explicitly out of scope for v0.1

- Online document editing, workflow designers, enterprise SSO / multi-tenancy
- Personal WeChat (only WeCom), UI automation / simulated clicks
- Any third-party relay cloud service: this gateway is self-hosted

## Roadmap

1. Week 1: skeleton + unified tool definitions + Feishu message / approval read + dry-run / audit
2. Week 2: Feishu approval decide (confirmation + idempotency) + DingTalk message + WeCom message
3. Week 3: DingTalk / WeCom approval read + cross-platform todo aggregation + demo
4. Week 4: calendar / document read-only + v0.1.0

## License

MIT
