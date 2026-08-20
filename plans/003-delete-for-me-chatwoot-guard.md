# Plan 003: Delete-for-me events reach Chatwoot in Chatwoot-only deployments

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> "STOP conditions" item occurs, stop and report — do not improvise. When done,
> update this plan's row in `plans/README.md` unless a reviewer told you they
> maintain the index.
>
> **Drift check (run first)**: `cd src && git diff --stat 49fcb34..HEAD -- infrastructure/whatsapp/event_handler.go infrastructure/whatsapp/event_message_handler.go`
> If either file changed, compare the "Current state" excerpts to the live code;
> on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `49fcb34`, 2026-07-06

## Why this matters

`handleDeleteForMe` only forwards the delete event when at least one generic
`WHATSAPP_WEBHOOK` URL is configured. In a **Chatwoot-only** deployment (no
generic webhook URL, but `ChatwootEnabled=true` with `ChatwootMessageDelete` /
`ChatwootForwardDeletes` on), the delete event never leaves the handler, so the
Chatwoot-side tombstone note / linked-message deletion silently never fires. The
message handler (`event_message_handler.go:166`) and the receipt handler already
OR in `config.ChatwootEnabled`; the delete-for-me guard is the odd one out.
Delete-for-everyone (REVOKE) is unaffected — it rides the message path.

## Current state

- `src/infrastructure/whatsapp/event_handler.go` — `handleDeleteForMe`. The guard
  at line 103 forwards the delete only for generic webhooks.

Excerpt (`event_handler.go:102-111`):

```go
	// Send webhook notification for delete event
	if len(config.WhatsappWebhook) > 0 {
		go func(c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardDeleteToWebhook(webhookCtx, evt, message, deviceID, c); err != nil {
				log.Errorf("Failed to forward delete event to webhook: %v", err)
			}
		}(client)
	}
```

The pattern to match — the message handler's combined guard
(`event_message_handler.go:166`):

```go
	if (len(config.WhatsappWebhook) > 0 || config.ChatwootEnabled) &&
		!strings.Contains(evt.Info.SourceString(), "broadcast") {
```

`forwardDeleteToWebhook` is what ultimately emits the `message.deleted` event
that the Chatwoot forward layer consumes (via `shouldForwardEventToChatwoot` /
`deleteLinkedChatwootMessage`).

## Commands you will need

| Purpose | Command                                       | Expected     |
|---------|-----------------------------------------------|--------------|
| Build   | `cd src && go build ./...`                    | exit 0       |
| Vet     | `cd src && go vet ./...`                       | exit 0       |
| Test    | `cd src && go test ./infrastructure/whatsapp/...` | ok, all pass |
| Format  | `cd src && gofmt -l infrastructure/whatsapp/event_handler.go` | no output |

## Scope

**In scope**: `src/infrastructure/whatsapp/event_handler.go` (the one guard in
`handleDeleteForMe`).

**Out of scope** (do NOT touch): `forwardDeleteToWebhook` itself; the message /
receipt handlers; any Chatwoot forward code. Only widen the guard.

## Git workflow

- Branch: `advisor/003-delete-for-me-chatwoot`
- Commit: `fix(chatwoot): forward delete-for-me events in Chatwoot-only setups`
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Widen the delete-for-me forward guard to include Chatwoot

Change the condition at `event_handler.go:103` from:

```go
	if len(config.WhatsappWebhook) > 0 {
```

to:

```go
	if len(config.WhatsappWebhook) > 0 || config.ChatwootEnabled {
```

Leave the goroutine body unchanged.

**Verify**: `cd src && go build ./...` → exit 0

### Step 2: Vet, test, format

**Verify**:
- `cd src && go vet ./...` → exit 0
- `cd src && go test ./infrastructure/whatsapp/...` → ok
- `cd src && gofmt -l infrastructure/whatsapp/event_handler.go` → no output

## Test plan

The delete handler is not unit-tested today and needs whatsmeow event fixtures +
a fake repo (tracked separately under the test-coverage findings). For this S
fix, verification is build/vet/test green and a grep confirming the widened
guard. Do not build a new event-fixture harness here.

**Verify**: `cd src && grep -n "config.WhatsappWebhook) > 0 || config.ChatwootEnabled" infrastructure/whatsapp/event_handler.go` → at least one match.

## Done criteria

- [ ] `cd src && go build ./...` exits 0
- [ ] `cd src && go vet ./...` exits 0
- [ ] `cd src && go test ./infrastructure/whatsapp/...` passes
- [ ] The guard grep in the test plan matches
- [ ] `git status` shows only `event_handler.go` modified
- [ ] `plans/README.md` status row updated

## STOP conditions

- The guard does not match the "Current state" excerpt (file drifted).
- `forwardDeleteToWebhook` turns out NOT to feed the Chatwoot delete path (read
  it and `shouldForwardEventToChatwoot` first; if delete events are gated out of
  Chatwoot elsewhere, report that instead of shipping a no-op).
- Tests fail for unrelated reasons.

## Maintenance notes

- If a third forwarding sink is added later, prefer extracting a single
  `shouldForwardEvents()` helper so all handlers share one condition instead of
  three copies of the `||` chain.
- Reviewer: confirm delete-for-everyone (REVOKE) behavior is unchanged — this
  plan only touches delete-for-me.
