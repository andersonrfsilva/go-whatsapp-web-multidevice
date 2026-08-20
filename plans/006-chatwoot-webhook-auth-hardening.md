# Plan 006: Harden the Chatwoot inbound webhook (drop query-secret, warn on unauthenticated, reject default HMAC key)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> "STOP conditions" item occurs, stop and report — do not improvise. When done,
> update this plan's row in `plans/README.md` unless a reviewer told you they
> maintain the index.
>
> **Drift check (run first)**: `cd src && git diff --stat 0acdc86..HEAD -- ui/rest/chatwoot.go config/settings.go cmd/rest.go infrastructure/whatsapp/webhook.go`
> If any of these changed, compare the "Current state" excerpts to the live code;
> on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW (non-breaking — chosen deliberately; see "Approach decision")
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `0acdc86` (branch `main`), refreshed 2026-07-17 (orig. `49fcb34`/`pr739`, 2026-07-06 — reconciled after `pr739`↔`main` merge; excerpts re-verified live)

## Approach decision (operator-chosen)

The operator chose the **non-breaking** hardening (over "deny by default" and
"explicit opt-out flag"). This plan therefore does NOT change the default that
allows an unauthenticated webhook when no secret is configured — it makes that
state loud and removes the parts that are pure security wins with zero breakage.
If a future maintainer wants deny-by-default, that is a separate, breaking change
requiring a release note.

## Why this matters

Three issues on the inbound Chatwoot webhook, which is registered BEFORE the
basic-auth middleware (so it is publicly reachable by design):

1. `chatwootWebhookAuthorized` accepts the secret from `c.Query("secret")` — a
   secret in a URL leaks into access logs, proxies, and browser history. Removing
   this acceptance path is a pure win.
2. When `ChatwootWebhookSecret` is empty (the default), the webhook is fully
   unauthenticated and nothing tells the operator. A loud startup warning closes
   the "silently open" gap without breaking anyone.
3. The outbound webhook HMAC signing key `WhatsappWebhookSecret` defaults to the
   literal `"secret"`; if webhooks are enabled without overriding it, every
   payload is signed with a publicly-known key, giving downstream consumers no
   real authenticity guarantee. Warn (do not hard-fail) when signing with the
   default.

## Current state

- `src/ui/rest/chatwoot.go` — `chatwootWebhookAuthorized` (lines 146-173; candidate list at 152-156):

```go
func chatwootWebhookAuthorized(c *fiber.Ctx) bool {
	secret := strings.TrimSpace(config.ChatwootWebhookSecret)
	if secret == "" {
		return true
	}

	for _, candidate := range []string{
		c.Get("X-Chatwoot-Webhook-Secret"),
		c.Get("X-Gowa-Chatwoot-Secret"),
		c.Query("secret"),
	} {
		if chatwootSecretMatches(secret, candidate) {
			return true
		}
	}
	// ... X-Hub-Signature-256 HMAC path ...
}
```

- `src/config/settings.go:102` — `ChatwootWebhookSecret = ""` (default empty;
  comment already notes inbound requests remain unauthenticated for backward
  compatibility).
- `src/config/settings.go:35` — `WhatsappWebhookSecret = "secret"` (default HMAC
  signing key). `WhatsappWebhook []string` is at `:34`.
- `src/infrastructure/whatsapp/webhook.go` — uses `WhatsappWebhookSecret` to sign
  the outbound `X-Hub-Signature-256` header.
- `src/cmd/rest.go` — where the REST server is wired up and the Chatwoot webhook
  route is registered (search for `"/chatwoot/webhook"` and
  `StartChatwootForwardRetryWorker`); this is the place to emit startup warnings
  after config is loaded.

Logging convention: `logrus` (already imported across `cmd/` and `ui/rest/`).

## Commands you will need

| Purpose | Command                                   | Expected      |
|---------|-------------------------------------------|---------------|
| Build   | `cd src && go build ./...`                | exit 0        |
| Vet     | `cd src && go vet ./...`                  | exit 0        |
| Test    | `cd src && go test ./ui/rest/... ./infrastructure/whatsapp/...` | ok |
| Format  | `cd src && gofmt -l ui/rest/chatwoot.go`  | no output     |

## Scope

**In scope**:
- `src/ui/rest/chatwoot.go` (remove the `c.Query("secret")` candidate)
- `src/cmd/rest.go` (startup warnings)
- `src/ui/rest/chatwoot_test.go` (test the query-secret is no longer accepted)

**Out of scope** (do NOT touch):
- The default value of `ChatwootWebhookSecret` — must remain `""` (non-breaking).
- The HMAC verification path (`X-Hub-Signature-256`) — it is correct.
- The header-based secret acceptance (`X-Chatwoot-Webhook-Secret`,
  `X-Gowa-Chatwoot-Secret`) — keep both.

## Git workflow

- Branch: `advisor/006-chatwoot-webhook-auth`
- Commit: `fix(security): drop query-secret acceptance and warn on unauthenticated Chatwoot webhook`
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Stop accepting the secret from the query string

In `chatwootWebhookAuthorized` (`chatwoot.go:152-156`), remove the
`c.Query("secret")` entry (currently line 155) from the candidate list so only
the two headers are accepted:

```go
	for _, candidate := range []string{
		c.Get("X-Chatwoot-Webhook-Secret"),
		c.Get("X-Gowa-Chatwoot-Secret"),
	} {
		if chatwootSecretMatches(secret, candidate) {
			return true
		}
	}
```

**Verify**: `cd src && grep -n 'c.Query("secret")' ui/rest/chatwoot.go` → no output

### Step 2: Warn at startup when the Chatwoot webhook is unauthenticated

In `src/cmd/rest.go`, after config is loaded and where the Chatwoot webhook route
is registered (near `app.Post(webhookPath, chatwootHandler.HandleWebhook)`), add:

```go
		if strings.TrimSpace(config.ChatwootWebhookSecret) == "" {
			logrus.Warn("SECURITY: Chatwoot inbound webhook is UNAUTHENTICATED (CHATWOOT_WEBHOOK_SECRET is empty). Anyone who reaches the webhook URL can trigger WhatsApp sends and message deletes. Set CHATWOOT_WEBHOOK_SECRET to require authentication.")
		}
```

Ensure `strings` and `logrus` are imported in `rest.go` (they almost certainly
are; confirm the import block).

**Verify**: `cd src && go build ./...` → exit 0

### Step 3: Warn at startup when outbound webhook signs with the default key

Still in `src/cmd/rest.go`, near where webhooks are set up, add a warning when a
generic webhook is configured but the signing secret is still the default. Use
explicit parentheses so boolean precedence is unambiguous:

```go
		if len(config.WhatsappWebhook) > 0 &&
			(strings.TrimSpace(config.WhatsappWebhookSecret) == "" || config.WhatsappWebhookSecret == "secret") {
			logrus.Warn("SECURITY: outbound webhooks are signed with the default/empty WHATSAPP_WEBHOOK_SECRET; downstream consumers cannot verify authenticity. Set a strong WHATSAPP_WEBHOOK_SECRET.")
		}
```

**Verify**: `cd src && go vet ./...` → exit 0

### Step 4: Test that the query secret is rejected

In `src/ui/rest/chatwoot_test.go`, add a test asserting that when a secret IS
configured, a request presenting it only via `?secret=` is NOT authorized, while
the same secret in the `X-Chatwoot-Webhook-Secret` header IS. Use Fiber's
`app.Test(httptest.NewRequest(...))` against a tiny handler that calls
`chatwootWebhookAuthorized`, or construct a `*fiber.Ctx` via `app.AcquireCtx` if
the existing tests already do so. Model naming/structure after the existing
`chatwoot_test.go` cases.

**Verify**: `cd src && go test ./ui/rest/... -run Chatwoot -v` → PASS

### Step 5: Full verification

**Verify**:
- `cd src && go test ./ui/rest/... ./infrastructure/whatsapp/...` → ok
- `cd src && gofmt -l ui/rest/chatwoot.go cmd/rest.go` → no output

## Test plan

- New test in `chatwoot_test.go`: secret-in-query rejected, secret-in-header
  accepted, no-secret-configured still returns authorized (documents the
  deliberate non-breaking default).
- Pattern: existing tests in `chatwoot_test.go`.
- Verification: `cd src && go test ./ui/rest/...` → all pass including new cases.

## Done criteria

- [ ] `cd src && go build ./...` exits 0
- [ ] `cd src && go vet ./...` exits 0
- [ ] `grep -n 'c.Query("secret")' ui/rest/chatwoot.go` returns nothing
- [ ] Both startup warnings compile and are reachable after config load
- [ ] `cd src && go test ./ui/rest/... ./infrastructure/whatsapp/...` passes with new tests
- [ ] `cd src && gofmt -l ui/rest/chatwoot.go cmd/rest.go` prints nothing
- [ ] `git status` shows only the in-scope files modified
- [ ] `plans/README.md` status row updated

## STOP conditions

- Any excerpt does not match the live code (drift).
- The Chatwoot webhook route turns out to sit BEHIND basic auth already (read
  `cmd/rest.go` route registration order); if so, the exposure is different —
  report before changing behavior.
- Adding a Fiber test context requires new test infrastructure that does not
  exist and cannot be added in a small diff — if so, ship Steps 1-3 and report
  Step 4 as deferred rather than fabricating a fragile test.

## Maintenance notes

- Follow-up (explicitly deferred, needs a release note): a `deny-by-default` mode
  or `CHATWOOT_ALLOW_UNAUTHENTICATED_WEBHOOK` opt-out flag. Not in this plan
  because the operator chose the non-breaking path.
- Reviewer: confirm the default `ChatwootWebhookSecret = ""` is unchanged and
  that header-based secrets still work — this plan must not break existing
  authenticated deployments.
