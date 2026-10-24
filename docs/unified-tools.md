# Unified tools

One tool name works across Feishu, DingTalk and WeCom. The adapter layer
normalizes platform differences (tokens, signatures, error codes, approval
field names) into these shapes.

## office.message.send

- Params: `platform` (feishu | dingtalk | wecom), `target` (user id or group id),
  `text`, `dry_run?`
- Returns: platform message id (or a dry-run preview when dry-run is on).

## office.approval.list

- Params: `platform?` (omit = all enabled platforms), `status?` (pending | approved | rejected),
  `limit?`
- Returns: normalized todo items sorted by time/urgency: approval id, platform,
  applicant, title, created time, key fields (amount, leave dates, etc. when available).

## office.approval.get

- Params: `platform`, `approval_id`
- Returns: full normalized approval detail: applicant, reason, key fields, current
  status, approver chain (when the platform exposes it).

## office.approval.decide

- Params: `platform`, `approval_id`, `decision` (approve | reject),
  `comment?`, `confirm` (must be `true`), `idempotency_key` (required)
- Safety: without `confirm=true` the call is refused. With a repeated
  `idempotency_key` the original result is returned and no second execution happens.
- Returns: decision receipt + audit log id.

## Phase 2 (read-only first)

- `office.calendar.list`: params `platform`, `start`, `end`; returns normalized events.
- `office.doc.search`: params `platform`, `query`; returns matching documents.
- `office.doc.read`: params `platform`, `doc_id`; returns document content.
- Calendar create and document editing, if added later, follow the same
  confirm + idempotency + audit rules as approval decide.
