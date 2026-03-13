# Plan: Slack attachment ingestion with local file paths (tmux-owned UX)

## Status
- Decision: accepted
- Scope: add Slack attachment intake support while keeping message envelope shape and pushing user-facing attachment UX to tmux
- Constraints:
  - Inbound Slack DM attachments should be exposed to downstream as local file paths
  - Paths exposed to downstream must be workspace-relative (e.g., `workspace/tmp/slack/attachments/...`)
  - Paths stored under `workspace/tmp/slack/attachments`
  - Storage retention: 30 days
  - Cleanup: daemon scans folder every 4 hours based on sidecar ingest timestamp
  - No changes to attachment-related UX logic in Slack gateway

## Goals
- Preserve current Slack text flow and extend it to process attachments from DM messages.
- Keep gateway transport-focused; tmux remains responsible for user-facing attachment states and messaging.
- Provide predictable, bounded storage for downloaded files.
- Keep implementation non-breaking for existing message flow.

## Current baseline
- Connector currently handles `MessageEvent` text only and sends text through
  `gateway.IncomingMessage{Channel, ChatID, UserID, Text}`.
- Message conversion and chunking already happen in `internal/slack/connector.go` via `SendMessage`.
- Message formatting behavior is documented in `workspace/SLACK_MESSAGE_FORMATTING.md`.

## Proposed architecture

### 1) Envelope extension
- Keep existing envelope shape and add an optional field:
  - `Attachments []string` (workspace-relative local paths)
- Add optional attachment outcome metadata for tmux UX decisions:
  - `AttachmentResults []AttachmentResult`
  - `AttachmentResult{Index, FileID, Filename, Outcome, Reason, Bytes}`
- Add explicit serialized key:
  - `attachments_failed` populated from failed attachment outcomes
  - `attachments_succeeded` populated from accepted attachment outcomes
  - each failed item must include machine-readable `reason_code`
  - launch reason codes: `unsupported_type`, `too_large`, `download_failed`, `corrupt`, `unauthorized`, `deduped`
- Delivery invariant:
  - always send an envelope even when all attachments are rejected
- Slack connector builds this field from downloaded attachment files.
- No behavioral changes to tmux message schema consumers beyond receiving additional attachment paths.

### 2) Slack connector responsibility
- Receive file-aware message events from Socket Mode.
- Validate incoming file metadata and download allowed attachments to local disk.
- For file downloads, use bot token in `Authorization: Bearer <token>` header only.
- Never place auth tokens in URL/query parameters; redact/suppress token-bearing values in logs.
- Return `IncomingMessage` with:
  - existing fields unchanged
  - `Text` preserved for captions
  - `Attachments` set to downloaded file paths
  - file-only messages supported: emit envelope with empty `Text` and populated attachment fields
- Keep attachment user-facing handling (success/failure messaging) out of gateway.

### 3) File ingestion and storage model
- Download directory: `workspace/tmp/slack/attachments`.
- File naming:
  - Use canonical safe stored name derived from deterministic hash/UUID, plus normalized extension.
  - Stored path must be rooted under attachments directory and emitted as workspace-relative path.
- Write strategy:
  - stream to a temp file under the same directory
  - enforce byte cap while streaming (not metadata-only)
  - `fsync` and atomic `rename` to final path before adding to envelope
  - never expose temp paths to downstream
- Keep original filename only for optional metadata/logging if needed; do not use it to open or write paths.
- Persist a sidecar metadata file per attachment as JSON (`.meta.json`) with immutable `ingested_at`, `file_id`, and stored relative path.
- Persist a checksum in sidecar JSON metadata (`sha256`) for audit/debug and future dedupe support.

### 4) Validation policy in gateway
- Allowed formats at launch: images, `csv`, `tsv`, `xlsx`, `docx`, `pdf`.
- Explicitly not supported at launch: archives (`zip`, `tar`, `gz`, `rar`, `7z`, etc.).
- Size limits:
  - max per attachment: 25 MB
  - max total attachments per message: 251 MB
- Type validation must be dual:
  - declared Slack metadata MIME/type check
  - content sniff/magic-byte check on downloaded bytes
- Corrupt/invalid file handling:
  - do not crash gateway
  - always emit per-attachment outcome in `AttachmentResults`
  - preserve text-only processing when attachments are rejected
- Record a simple attachment status/result per file to aid observability.

### 5) TTL cleanup job
- Daemon-only task scans `workspace/tmp/slack/attachments` every 4 hours.
- Remove files whose age is greater than 30 days (`now - ingested_at > 30d` from sidecar metadata).
- Ensure both content and companion metadata files (if any) are removed together.
- Cleanup must skip files with active lock markers (if present) to avoid races.

## Implementation plan

### Phase 1: Data contract and config
1. Extend internal message envelope type used by `gateway` to include `Attachments []string`.
2. Add `AttachmentResults []AttachmentResult` to support tmux-owned UX without gateway copy decisions.
3. Add serialized envelope key `attachments_failed` for rejected attachments.
4. Add serialized envelope key `attachments_succeeded` for accepted attachments.
5. Add config knobs (if not already present) for:
   - attachment allowed MIME/types list
   - max attachment size (25 MB default)
   - max aggregate attachment size per message (251 MB default)
   - attachment root path
   - max parallel downloads per message (default: 3)
6. Document expected behavior for unsupported/noisy attachments in `workspace/SLACK_MESSAGE_FORMATTING.md`:
   - each reject reason code must map to a user-facing message template
   - include reason code in agent-facing `attachments_failed` payload
   - include accepted vs failed attachment summary in user-facing message copy

### Phase 2: Connector changes
1. Detect file-bearing message events in Slack connector.
2. For each attachment:
   - validate metadata early (including size/type pre-check)
   - download bytes with bot-token auth and enforce streaming byte cap
   - validate content type by sniffing bytes
   - atomically persist under `workspace/tmp/slack/attachments`
   - append workspace-relative path to envelope `Attachments` on success
   - append structured reject/accept outcome to `AttachmentResults`
3. Handle failure per file with non-fatal behavior and continue processing all remaining attachments.
4. If aggregate size cap is exceeded, reject only attachments that exceed remaining budget; keep earlier valid accepted files.
   - ordering policy: evaluate in original Slack attachment order (first-come, first-kept)
5. Always emit the message envelope; include `attachments_failed` and `attachments_succeeded` when present.
6. Ensure tmux can distinguish per-file success vs failure outcomes.
7. Forward message to handler unchanged except additional `Attachments`.
8. Idempotency: dedupe using stable Slack delivery identity (`event_id` when available), with fallback composite key (`team_id + channel_id + message_ts + file_id`).
   - deduped files/events must be emitted in `attachments_failed` with `reason_code=deduped`

### Phase 3: Cleanup subsystem
1. Add/extend a daemon task in the Slack-facing process to run every 4 hours.
   - ownership: daemon only (no connector-local sweeper)
2. Iterate attachment root and delete files where `now - ingested_at > 30d` from sidecar metadata.
3. Emit counters/logs:
   - scanned
   - deleted
   - skipped (recent)
   - skipped (locked/in-use)
   - errors

### Phase 4: Observability + docs
1. Log per-message attachment summary:
   - `attachments_count`
   - accepted/skipped counts
   - total bytes saved
2. Update README/ops docs where current Slack behavior is described.
3. Add a reason-code mapping table for tmux UX copy:
   - `unsupported_type` -> unsupported file type
   - `too_large` -> file exceeds size limit
   - `download_failed` -> download failed, retry suggested
   - `corrupt` -> file could not be parsed/decoded
   - `unauthorized` -> permission/auth issue accessing file
   - `deduped` -> duplicate delivery skipped

### Phase 5: Acceptance and rollout
1. Non-regression: text-only DM messages unchanged.
2. Positive attachment paths: upload image, receive processing envelope with valid local paths.
3. Rejects:
   - unsupported format
   - oversized file
   - aggregate size over message cap
   - corrupt/non-decodable file
   - partial-accept behavior when aggregate cap exceeded (accepted and rejected files both reported)
4. Cleanup verification: files older than 30 days removed on 4-hour sweep.

## Edge cases and design choices
- `File events vs text events`: ensure file-share message subtypes are captured without breaking text-only behavior.
- `Dedupe`: avoid duplicate processing on retried Slack events using `event_id` or fallback composite key.
- `Path hygiene`:
  - compute `filepath.Clean`
  - resolve symlinks/canonical path
  - enforce prefix/root match against attachment directory before passing to handler
- `Caption only messages`: allow text-only and file-only combinations independently.
- `Large volumes`: add bounded concurrency for downloads if needed.

## Risks and mitigations
- Risk: malformed attachment metadata from Slack API.
  - Mitigation: strict parser and fallback skip with explicit error status.
- Risk: cleanup race with active processing.
  - Mitigation: delete only strictly older-than-30-day files; optional open-file check before delete if needed.
- Risk: file `mtime` drift causes nondeterministic retention.
  - Mitigation: base TTL strictly on sidecar `ingested_at`, not `mtime`.
- Risk: unsupported attachments degrade user trust.
  - Mitigation: tmux owns clear copy responses with actionable guidance.

## Open questions
