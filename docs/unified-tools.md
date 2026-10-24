# Unified tools

One tool name works across Feishu, DingTalk and WeCom. The adapter layer
normalizes platform differences (tokens, signatures, error codes, approval
field names) into these shapes. The same tools are available over HTTP
(`POST /tools/<name>`) and over MCP stdio (`tools/call`).

## office.message.send

- Params: `platform` (feishu | dingtalk | wecom), `target` (user id, or a
  Feishu group id starting with `oc_`), `text`
- Returns: `{platform, message_id, dry_run}`. With `dry_run: true` in
  config, `message_id` is `"dry-run"` and no platform call is made.

## office.approval.list

- Params: `platform?` (omit = all enabled platforms), `status?`
  (pending | approved | rejected)
- Returns: normalized approvals sorted newest first: id, platform, title,
  applicant, status, created time.
- A platform missing its approval config (Feishu `user_id`, DingTalk
  `process_code`) is skipped when listing all platforms, and returns a
  clear error when requested on its own.

## office.approval.get

- Params: `platform`, `approval_id`
- Returns: one normalized approval.

## office.approval.decide

- Params: `platform`, `approval_id`, `decision` (approve | reject),
  `comment?`, `confirm` (must be `true`), `idempotency_key` (required)
- Safety, in order: validate decision → require confirm and key →
  replay a stored result for a repeated key → dry-run preview (key is
  NOT consumed) → real platform call → store result + audit entry.
- Returns: `{platform, approval_id, decision, receipt, dry_run,
  idempotency_key, replayed}`.
- DingTalk and WeCom decide are refused with a clear error in v0.1
  (see README platform status); a refusal never consumes the key.

## office.calendar.list (read-only)

- Params: `platform`, `start`, `end` (RFC 3339)
- Returns: normalized events. Feishu needs `calendar_id` in config.

## office.doc.search / office.doc.read (read-only)

- Params: `platform`, `query` / `doc_id`
- Returns: matching documents / one document. Feishu only in v0.1.
