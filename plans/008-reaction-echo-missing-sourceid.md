# Plan 008: Stamp reaction-mirror Chatwoot messages with SourceID to close the echo race

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md`.
>
> **Drift check (run first)**:
> `git diff --stat f4df311..HEAD -- src/infrastructure/whatsapp/webhook_forward.go`
> The "Current state" excerpts below were read from the working tree at commit
> `f4df311` and re-verified during this revision. If the diff shows changes to
> the `message.reaction` case, compare against the excerpt below before
> proceeding — a mismatch is a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S (one-line fix + one test)
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `f4df311`, 2026-08-20
- **Revised**: 2026-08-20 (facts re-verified against source; see "Revision notes")

## Why this matters

When a WhatsApp user reacts to a message (e.g. 👍), GOWA mirrors that reaction
into Chatwoot as a synthetic note like `"Alice reacted 👍 to message
3EB0...XX"` (built by `buildReactionChatwootContent`,
`src/infrastructure/whatsapp/webhook_forward.go:543`). When the reactor is the
device owner, this note is created in Chatwoot as an **outgoing** message
(`webhook_forward.go:1184`: `info.IsFromMe = chatwootMessageTypeFromPayload(data) == "outgoing"`).

Chatwoot then fires a `message_created` webhook back at GOWA for that outgoing
message. The inbound handler (`src/ui/rest/chatwoot.go`) decides whether it is
a real agent reply (send to WhatsApp) or GOWA's own echo (drop). It has exactly
three filters, in this order:

- `chatwoot.go:318` — `if payload.Private` → drop. Reaction notes are **not**
  private (`msgOpts.Private` is never set on this path), so this does not help.
- `chatwoot.go:322` — `chatwoot.IsMessageSentByUs(payload.ID)`, an in-memory
  cache populated by `MarkMessageAsSent(msgID)` **after** the Chatwoot create
  call returns (`webhook_forward.go:843`). If Chatwoot delivers the webhook
  before that line runs, this misses. That is the observed race.
- `chatwoot.go:331` — `isEchoOfForwardedMessage(payload)`, which is
  timing-independent because `source_id` is part of the create request itself:

  ```go
  func isEchoOfForwardedMessage(payload chatwoot.WebhookPayload) bool {
      return strings.HasPrefix(payload.SourceID, "WAID:")
  }
  ```

The `message.reaction` case is the only outgoing-note path that never sets
`msgOpts.SourceID`, so filter 3 cannot fire for it. When filter 2 loses the
race, the synthetic reaction text is sent back out to the WhatsApp user as if
it were an agent message. That is the reported bug: users see literal
"Anderson/Mira TI reacted 👍 to message 3EB0B190023254D93B88F4" in their chat.

The fix is a one-line addition mirroring the `default` case in the same switch.

## Current state

Verified reads (commit `f4df311`):

- `src/infrastructure/whatsapp/webhook_forward.go:1136-1147` — the case to
  change:

  ```go
  case "message.reaction":
      rid, _ := data["reacted_message_id"].(string)
      content = buildReactionChatwootContent(data, info.FromName, reactedMessagePreview(deviceID, linkRepo, rid))
      if rid != "" {
          // Thread the reaction onto the message it references. Besides the
          // WAID external id (which only resolves for WAID-stamped contact
          // messages), resolve the native in_reply_to via the message-link
          // table so reactions to agent-authored messages (which carry no WAID
          // source_id) also attach to the original in Chatwoot.
          attrs, _ := buildChatwootReplyAttributes(deviceID, linkRepo, rid)
          msgOpts.ContentAttributes = attrs
      }
  ```

  `rid` is the id of the message being reacted *to*. It is NOT the id of the
  reaction event and must never be used as the SourceID — it would collide with
  the SourceID already stamped on the original message when it was forwarded.

- `src/infrastructure/whatsapp/webhook_forward.go:1158-1166` — the `default`
  case, the pattern this fix mirrors:

  ```go
  default:
      content, attachments = buildChatwootMessageContent(data, info.IsGroup, info.FromName)
      if content == "" && len(attachments) == 0 {
          logrus.Infof("Chatwoot: Skipping %s with no renderable content", eventName)
          return nil
      }
      if id, _ := data["id"].(string); id != "" {
          msgOpts.SourceID = "WAID:" + id
      }
  ```

- **`data["id"]` is the reaction's own WhatsApp message id — already verified,
  no investigation needed.** In `src/infrastructure/whatsapp/event_message.go`,
  `buildEventPayload` sets the common field first:

  ```go
  payload["id"] = evt.Info.ID          // event_message.go:97
  ```

  and only later, in the reaction branch, the separate field:

  ```go
  if reactionMessage := msg.GetReactionMessage(); reactionMessage != nil {
      payload["reaction"] = reactionMessage.GetText()
      if key := reactionMessage.GetKey(); key != nil {
          payload["reacted_message_id"] = key.GetID()   // event_message.go:158
      }
      return EventTypeMessageReaction, payload, nil
  }
  ```

  `evt.Info.ID` (the reaction message's own id) and
  `reactionMessage.GetKey().GetID()` (the reacted-to message) are distinct
  sources. There is no code path where `payload["id"]` is overwritten with
  `reacted_message_id`.

- **Stamping SourceID on reactions creates no message-link rows.** The
  link-table upsert is gated on `eventName == "message"`
  (`webhook_forward.go:1203-1210`), so a reaction's SourceID never reaches
  `UpsertChatwootMessageLink` and cannot pollute later
  `buildChatwootReplyAttributes` / `reactedMessagePreview` lookups. This fix
  changes exactly one thing: the `source_id` field on the Chatwoot create call.

- `msgOpts` is `chatwoot.MessageOptions`, already in scope; `SourceID` is a
  plain `string` field (see `webhook_forward.go:889`).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `cd src && go build ./...` | exit 0 |
| Vet | `cd src && go vet ./...` | exit 0, no output |
| Test | `cd src && go test ./...` | all pass |
| Format check | `cd src && gofmt -l infrastructure/whatsapp/webhook_forward.go infrastructure/whatsapp/chatwoot_forward_test.go` | no output |

## Scope

**In scope**:

- `src/infrastructure/whatsapp/webhook_forward.go` (the one-line fix)
- `src/infrastructure/whatsapp/chatwoot_forward_test.go` (the new test)

**Out of scope** (do NOT touch, even though they look related):

- `src/ui/rest/chatwoot.go` — the echo guard is correct and generic; this fix
  only feeds it the SourceID it already knows how to check.
- `buildChatwootReplyAttributes` / `ContentAttributes` handling — unrelated to
  the echo bug, already correct, do not restructure.
- The `message.edited` / `message.revoked` / `message.deleted` case
  (`webhook_forward.go:1148-1157`). It has the **same** missing-SourceID gap,
  but fixing it is NOT safe as a one-liner (see "Known adjacent gap" below) and
  is tracked separately as finding 010 in `plans/README.md`.
- Any production code change made only to make testing easier — if the test
  seems to require a new seam, that is a STOP condition.

## Known adjacent gap (do not fix here)

The `message.edited`/`revoked`/`deleted` case sets only `ContentAttributes`:

```go
if threadID != "" {
    msgOpts.ContentAttributes = map[string]any{"in_reply_to_external_id": "WAID:" + threadID}
}
```

It never sets `msgOpts.SourceID`, so tombstone/edit notes have the same echo
exposure as reactions do today. It is excluded from this plan because for edit
events `data["id"]` (`evt.Info.ID`) versus `data["original_message_id"]`
(`protocolMessage.GetKey().GetID()`, `event_message.go:141`) can coincide
depending on the sending client, which risks a `source_id` collision with the
already-forwarded original. That needs its own investigation and its own plan.

## Git workflow

- Branch: `advisor/008-reaction-echo-sourceid`, based on `main` at `f4df311`.
  The reply-threading work that was previously an uncommitted diff is already
  committed as `f4df311` — no working-tree diff needs preserving in `src/`.
- Single commit, repo convention (`git log --oneline -5`), e.g.
  `fix(chatwoot): stamp reaction echoes with SourceID to close send-back race`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add SourceID to the `message.reaction` case

In `src/infrastructure/whatsapp/webhook_forward.go`, inside
`syncPayloadToChatwoot`, extend `case "message.reaction":` to stamp
`msgOpts.SourceID` from the reaction event's own id, independent of the
`rid != ""` branch (a reaction with no resolvable reacted-message id still
needs the echo guard). Target shape:

```go
case "message.reaction":
    rid, _ := data["reacted_message_id"].(string)
    content = buildReactionChatwootContent(data, info.FromName, reactedMessagePreview(deviceID, linkRepo, rid))
    // The reaction's own WhatsApp id (not reacted_message_id) — without this
    // stamp the outgoing note is invisible to isEchoOfForwardedMessage and can
    // be sent back to WhatsApp when the sent-message cache loses the race.
    if id, _ := data["id"].(string); id != "" {
        msgOpts.SourceID = "WAID:" + id
    }
    if rid != "" {
        // Thread the reaction onto the message it references. Besides the
        // WAID external id (which only resolves for WAID-stamped contact
        // messages), resolve the native in_reply_to via the message-link
        // table so reactions to agent-authored messages (which carry no WAID
        // source_id) also attach to the original in Chatwoot.
        attrs, _ := buildChatwootReplyAttributes(deviceID, linkRepo, rid)
        msgOpts.ContentAttributes = attrs
    }
```

Do not change `rid`'s meaning or the `ContentAttributes` logic.

**Verify**: `cd src && go build ./... && go vet ./...` → both exit 0.

### Step 2: Add the regression test

There is currently **no** test that exercises `syncPayloadToChatwoot`; the
existing reaction tests (`chatwoot_forward_test.go:355`,
`TestBuildReactionChatwootContent`) only cover the content builder, so they
cannot catch this bug. Add one test that drives the real function against a
fake Chatwoot HTTP server and asserts the `source_id` on the message-create
request body.

What makes this feasible without touching production code:

- `getChatwootClientFn` (`webhook_forward.go:22`) is a package-level `var` you
  can swap in the test and restore with `defer`.
- `chatwoot.Client` is a plain struct; a literal
  `&chatwoot.Client{BaseURL: server.URL, APIToken: "t", AccountID: 1, InboxID: 1, HTTPClient: server.Client()}`
  satisfies `IsConfigured()` (`infrastructure/chatwoot/client.go:147`).
- Call `syncPayloadToChatwoot(context.Background(), payload, "message.reaction", "", nil)`
  — `deviceID: ""` and `linkRepo: nil` skip all link-table work.

Model the fake server on `src/infrastructure/chatwoot/client_reopen_test.go`
(the closest existing full-flow httptest fixture: contact search/create →
conversation list/create → message create). Endpoints your handler must serve,
with `AccountID: 1`:

| Method | Path | Minimal response |
|--------|------|------------------|
| GET | `/api/v1/accounts/1/contacts/search` | `{"payload": [{"id": 7, ...}]}` |
| GET | `/api/v1/accounts/1/contacts/7/conversations` | an open conversation with `"id": 42` |
| POST | `/api/v1/accounts/1/conversations/42/messages` | capture the decoded request body, respond `{"id": 999}` |

Read the exact response shapes the client decodes from
`infrastructure/chatwoot/client.go` (`FindOrCreateContact`,
`FindOrCreateConversation`, `CreateMessage`) — do not guess field names.

Payload fixture (outgoing reaction):

```go
payload := map[string]any{"payload": map[string]any{
    "id":                 "REACTION-ID-1",
    "reacted_message_id": "ORIGINAL-ID-1",
    "reaction":           "👍",
    "from":               "628123456789@s.whatsapp.net",
    "chat_id":            "628123456789@s.whatsapp.net",
    "from_name":          "Alice",
    "is_from_me":         true,
}}
```

Assertions:

- the captured message-create body has `source_id == "WAID:REACTION-ID-1"`
- it is **not** `"WAID:ORIGINAL-ID-1"` (assert this explicitly — it is the
  failure mode a careless fix produces)
- `message_type == "outgoing"`

**Verify**: `cd src && go test ./infrastructure/whatsapp/... -run Reaction -v`
→ the new test passes. Then revert Step 1 locally (or temporarily comment the
new `if id, _ :=` block), re-run, and confirm the new test **fails** — a test
that passes without the fix is worthless. Restore Step 1 afterwards.

### Step 3: Full suite and format check

**Verify**:

- `cd src && go test ./...` → all pass
- `cd src && gofmt -l infrastructure/whatsapp/webhook_forward.go infrastructure/whatsapp/chatwoot_forward_test.go` → empty output

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `cd src && go build ./...` exits 0
- [ ] `cd src && go vet ./...` exits 0, no output
- [ ] `cd src && go test ./...` exits 0
- [ ] `cd src && gofmt -l infrastructure/whatsapp/webhook_forward.go infrastructure/whatsapp/chatwoot_forward_test.go` empty
- [ ] `grep -n 'case "message.reaction":' -A 10 src/infrastructure/whatsapp/webhook_forward.go` shows `msgOpts.SourceID = "WAID:" + id`
- [ ] The new test was confirmed to fail without the Step 1 change (report the observed failure message)
- [ ] `git status` shows only `src/infrastructure/whatsapp/webhook_forward.go`, `src/infrastructure/whatsapp/chatwoot_forward_test.go`, and `plans/README.md` modified
- [ ] `plans/README.md` status row for 008 updated

## STOP conditions

Stop and report back (do not improvise) if:

- The code at `webhook_forward.go:1136-1147` does not match the "Current state"
  excerpt above.
- Wiring the Step 2 test would require changing production code (adding a seam,
  exporting a symbol, splitting a function). Report what blocked it; a manual
  verification note is acceptable instead of a forced refactor.
- The fake-server fixture balloons past ~100 lines or needs endpoints beyond the
  three listed. Report the extra calls the client makes rather than stubbing
  blindly.
- `go test ./...` has pre-existing failures unrelated to this change (note them;
  do not fix unrelated failures in this plan).

## Maintenance notes

- This closes the race only for *outgoing* (device-owner) reactions, the only
  direction that can echo back to WhatsApp. Incoming (customer) reactions become
  `incoming` Chatwoot messages, which `chatwoot.go:316` drops before any echo
  guard runs — never at risk.
- **Correction to the pre-revision version of this plan**: edits/deletes do NOT
  already stamp SourceID. `webhook_forward.go:1156` sets `ContentAttributes`, not
  `SourceID`. See "Known adjacent gap" and finding 010 in `plans/README.md`.
- Rule for future work: any new switch case in `syncPayloadToChatwoot` that can
  produce a non-private **outgoing** Chatwoot message must set `msgOpts.SourceID`
  from that event's own WhatsApp id, or it inherits this bug.
- Relates to direction item DIR-02 in `plans/README.md` ("relay agent reactions
  back to WhatsApp") — a different feature (agent-initiated reactions), not this
  bug; do not conflate.

## Revision notes (2026-08-20)

What changed versus the first draft, and why:

1. **Resolved the open STOP condition about `data["id"]`.** Verified in
   `event_message.go:97,158` that it is `evt.Info.ID`, distinct from
   `reacted_message_id`. The executor no longer has to investigate this.
2. **Corrected a false maintenance claim** that edits/deletes "already handle
   this correctly at :1156" — they do not stamp SourceID at all. Recorded as a
   separate finding rather than silently widening this plan's scope.
3. **Replaced the guesswork test plan.** The first draft assumed existing
   `msgOpts` coverage in a `webhook_forward_test.go`; no such file or coverage
   exists. Step 2 now names the real file, the real seam (`getChatwootClientFn`),
   the real fixture to copy (`client_reopen_test.go`), and requires proving the
   test fails without the fix.
4. **Documented the no-side-effects finding** (link-table upsert is gated on
   `eventName == "message"`), pre-empting the natural "does this pollute the
   message-link table?" objection.
5. **Fixed the stale Git workflow note** about basing on an uncommitted working
   tree — that diff is committed as `f4df311`.
6. **Spelled out the full echo-guard chain** with line numbers
   (`chatwoot.go:318,322,331`) so the executor understands why guards 1 and 2
   do not cover this path.
