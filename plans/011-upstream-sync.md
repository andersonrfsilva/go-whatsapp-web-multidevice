# Plan 011: Sync local `main` with `origin/main` (45 upstream commits, 3 breaking changes)

> **Executor instructions**: This plan is NOT a mechanical fix. It is a merge
> with judgment calls, and it has more STOP conditions than any other plan in
> this directory. Read the whole file before running a single command. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition fires, stop and report — do not improvise.
>
> **This plan requires a capable executor.** If you find yourself guessing what
> a piece of Chatwoot code is for, stop and report instead of resolving the
> conflict by feel. A wrong resolution here silently breaks a production
> integration that has almost no automated test coverage.
>
> **The three operator decisions are ANSWERED** (2026-08-20) — see "Decisions"
> below. Follow upstream on `src/views`, migrate to Fiber v3, merge all 45
> commits in one go.

## Status

- **Priority**: P0 for the fork's health (every other `webhook_forward.go` plan
  is blocked behind it), but **not urgent** — nothing is on fire today.
- **Effort**: L
- **Risk**: HIGH
- **Depends on**: nothing in `plans/`, but blocks any future plan touching
  `webhook_forward.go`, `ui/rest/chatwoot.go`, or `infrastructure/chatwoot/`.
- **Category**: tech debt / migration
- **Planned at**: commit `72783ce`, 2026-08-20

## Why this matters

`origin` is the upstream project (`aldinokemal/go-whatsapp-web-multidevice`);
there is no fork remote and no push access. All local work lives on `main`
locally, which has now diverged badly:

- **19 commits ahead, 45 behind.** Merge base: `276c8d7`
  (`feat(message): add forward message by ID (#394) (#755)`).
- The gap grows every week, and it concentrates in exactly the files the local
  work lives in (the Chatwoot layer). The longer this waits, the more expensive
  it gets — this is the "fork-drift cluster" already catalogued in
  `plans/README.md` Wave 3.
- Concretely, it is already blocking work: plan 008 was executed against local
  `f4df311`, but upstream `b8043af` rewrote the same code block, so that fix
  will have to be re-applied by hand after this merge.

## Current state (measured, not guessed)

A dry-run merge (`git merge-tree --write-tree main origin/main` — read-only,
touched nothing) reports **7 files in conflict**:

| File | Local churn since base | Upstream churn since base |
|---|---|---|
| `AGENTS.md` | +78 / -137 | +12 / -12 |
| `src/infrastructure/chatwoot/client.go` | +147 / -13 | +85 / -42 |
| `src/infrastructure/chatwoot/client_methods_test.go` | +214 / -5 | +49 / -85 |
| `src/infrastructure/whatsapp/chatwoot_forward_test.go` | +296 / -1 | +38 / -4 |
| `src/infrastructure/whatsapp/event_message.go` | +44 / -0 | +162 / -0 |
| `src/infrastructure/whatsapp/webhook_forward.go` | +161 / -11 | +274 / -19 |
| `src/ui/rest/chatwoot.go` | +106 / -22 | +311 / -57 |
| `src/usecase/send.go` | +11 / -6 | +31 / -14 |

**The textual conflicts are the easy part.** Three upstream changes rewrite the
ground underneath them:

1. **`bbfe75c` — Fiber v2 → v3.** Local `src/go.mod` is on
   `github.com/gofiber/fiber/v2 v2.52.13`; upstream is on
   `github.com/gofiber/fiber/v3 v3.4.0` (plus `gofiber/contrib/v3/websocket`,
   `gofiber/utils/v2`). **24 Go files** in the tree import Fiber; **3 of them**
   carry local modifications. Files git auto-merges "cleanly" will still fail to
   compile. Budget real time for this phase — it is not conflict resolution, it
   is an API migration.
2. **`80f3061` — `feat!:` standalone `gowa-ui` dashboard.** Upstream deleted
   `src/views/` entirely; the admin UI is now a separate artifact downloaded at
   runtime. Local `main` still has `src/views/`. Local never modified it, so git
   will delete it silently with no conflict marker. This changes how the app is
   served in deployment — see decision 1.
3. **`93aaaab` — Chatwoot multi-device / multi-inbox support (#696, #710).**
   This is the single biggest source of conflict, rewriting large parts of
   `ui/rest/chatwoot.go` and `infrastructure/chatwoot/client.go`. It overlaps
   conceptually with the local direction item DIR-01 (per-device Chatwoot
   inboxes) — upstream may have already built what DIR-01 wanted.

### Who changed what, per conflicting file

Use this to decide which side owns the shape of each file.

**`src/infrastructure/whatsapp/webhook_forward.go`**
- local: `f9bfd51` (SourceID stamp on reaction echoes — plan 008),
  `f4df311` (native `in_reply_to` for reaction notes via message-link table),
  `0acdc86` (thread quoted replies WhatsApp↔Chatwoot), `889667d` (media
  filename display name + mime fallback), `49fcb34` (render polls, skip
  unrenderable events)
- upstream: `b8043af` (drop redundant WhatsApp id from reaction notes),
  `3305bd9` (render InteractiveMessage / CTA buttons as text), `93aaaab`
  (multi-device/multi-inbox), `9ee1629` (`WHATSAPP_WEBHOOK_IGNORE_JIDS`)

**`src/ui/rest/chatwoot.go`**
- local: `2e7c95f` (quote inbound contact messages in agent replies),
  `0acdc86` (quote threading), `9096d11` (fallback to `contact.Identifier`
  when `PhoneNumber` is empty on group sends)
- upstream: `bbfe75c` (Fiber v3), `e69626f` (group chat send fix, #739),
  `93aaaab` (multi-device/multi-inbox)

**`src/infrastructure/chatwoot/client.go`**
- local: `69ac2e8` (page backward to resolve quoted message `source_id`),
  `3dde024` (decode messages list with numeric `message_type`), `2e7c95f`,
  `889667d`
- upstream: `29ebb77` (propagate device context to async forward goroutines),
  `3a9a18a` (remove deadcode), `93aaaab`

**`src/infrastructure/whatsapp/event_message.go`**
- local: `49fcb34` only (poll rendering) — a small additive delta
- upstream: `29ebb77`, `3305bd9`, `91d27b1` (sender display names) — all
  additive too. Expect this one to resolve side-by-side.

**`src/usecase/send.go`**
- local: `5ec8ec2` (clean up SendImage temp files on error paths)
- upstream: `746fdc1` (prefix send-image temp files with a UUID), `80f3061`,
  `bbfe75c`, `4ef5c4f` (send link storage, #661)
- **These two fixes are complementary, not duplicates — verified.** Local adds a
  `defer` that cleans `deletedItems` on every return path (previously the
  cleanup goroutine only ran on the success path). Upstream prefixes the temp
  path with a UUID so two concurrent sends of the same filename stop sharing one
  path on disk. **Both must survive the merge.** Do not drop either.

## Decisions — ANSWERED by the operator on 2026-08-20

All three are settled. Do not re-open them, do not ask again, and do not
second-guess them mid-merge.

1. **`src/views/` — FOLLOW UPSTREAM.** Let the merge delete the directory. The
   admin dashboard becomes the separately-downloaded `gowa-ui` artifact. Do NOT
   resurrect `src/views/`, and do not "helpfully" preserve any file under it.
   Consequence to record but not act on: the Wave-2 finding 008 in
   `plans/README.md` (CDN assets with no SRI in `src/views/index.html`) becomes
   moot — the reviewer will mark it REJECTED, not you.
2. **Fiber v2.52.13 → v3.4.0 — ACCEPTED.** Migrate. Mechanical signature
   changes only; a v3 API that forces a *behavior* change is still a STOP
   condition.
3. **Scope — ALL 45 COMMITS AT ONCE.** A single `git merge origin/main`. Do not
   stage it, do not stop at an intermediate upstream commit. If this turns out
   to be unworkable, that is a STOP-and-report, not a licence to re-plan the
   merge into phases yourself. The `pre-upstream-sync` tag is the way back.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `cd src && go build ./...` | exit 0 |
| Vet | `cd src && go vet ./...` | exit 0, no output |
| Test | `cd src && go test ./...` | all pass |
| Race | `cd src && go test -race ./...` | all pass |
| Format | `cd src && gofmt -l .` | see note below |
| Conflict list | `git diff --name-only --diff-filter=U` | files still unresolved |

Note on `gofmt`: `plans/README.md` records pre-existing gofmt drift in
`event_label_test.go` and `webhook_forward.go` (Wave-2 finding 004). Do not
treat pre-existing drift as caused by this merge, and do not fix it here —
that is finding 004's job. Only ensure you introduce no *new* drift.

Go commands always run from `src/` — the repo root is not the Go module
(`AGENTS.md`).

## Scope

**In scope**: whatever the merge touches. This plan is the one exception to the
usual narrow file list — but every change must be attributable to either
(a) resolving a conflict, (b) fixing a Fiber v3 compile break, or (c) restoring
a local feature the merge dropped. Anything else is out of scope.

**Out of scope**:
- Any refactor, cleanup, or improvement noticed along the way. Write it down in
  NOTES; do not do it. A merge this size must not carry passengers.
- Re-applying plan 008's fix onto the merged shape (see "After this lands").
- Fixing the pre-existing gofmt drift (finding 004).
- Pushing anything to `origin`. **`AGENTS.md` states this explicitly: never push
  to upstream; local work lives only on local `main`.** No push, no PR, ever.

## Git workflow

**Escape hatch first. Both commands, before anything else:**

```bash
git fetch origin              # the "45 behind" figure is from the last fetch; there may be more
git tag pre-upstream-sync main
```

The tag is a one-command way back to today's state. Confirm it exists
(`git tag -l pre-upstream-sync`) before continuing.

Then work on a throwaway branch so `main` never moves until everything is green:

```bash
git switch -c sync/upstream-2026-08-20
git merge origin/main
```

`main` is only fast-forwarded onto the sync branch at the very end, and only
after every gate passes.

**Merge, not rebase — deliberate.** Rebase would replay 19 local commits onto
upstream and force you to re-resolve the same Chatwoot-layer conflict once per
commit that touched those files (several do). Merge resolves it once. The usual
argument for rebase — linear published history — does not apply: this history is
never pushed anywhere, since `origin` is the upstream repo and there is no push
access.

## Steps

### Step 0: Escape hatch and branch

Run the four commands in "Git workflow" above.

**Verify**: `git tag -l pre-upstream-sync` prints the tag;
`git branch --show-current` prints `sync/upstream-2026-08-20`;
`git status` shows the merge in progress with conflicts.

**STOP** if `git fetch` reveals substantially more than 45 upstream commits —
re-measure with `git rev-list --left-right --count main...origin/main` and
report the new numbers before proceeding, since the conflict analysis in this
plan was computed at 19/45.

### Step 1: Wave 1 — the trivial conflict

`AGENTS.md`: keep the local version wholesale. Upstream's is the generic fork
pointer; the local one is the maintained agent-invariants document.

```bash
git checkout --ours AGENTS.md && git add AGENTS.md
```

**Verify**: `git diff --name-only --diff-filter=U` no longer lists `AGENTS.md`;
`head -3 AGENTS.md` shows the local heading (`# gowa — invariantes de agente`).

### Step 2: Wave 2 — production code, Chatwoot layer

Resolve in this order: `event_message.go`, `usecase/send.go`,
`infrastructure/chatwoot/client.go`, `webhook_forward.go`, `ui/rest/chatwoot.go`.
Easiest first, so you learn the shape of upstream's refactor on small files
before hitting the big ones.

**Governing rule for the three big Chatwoot files** (`client.go`,
`webhook_forward.go`, `ui/rest/chatwoot.go`): **take upstream's shape as the
base and re-apply the local features on top**, not the reverse. Upstream changed
more than local in all three, and staying close to upstream's structure is what
makes the *next* sync cheaper. Resolving in the other direction is how a fork
becomes permanently unmergeable.

Local features that MUST survive — verify each one by name after resolving:

| Feature | Where it lives | Local commit |
|---|---|---|
| Reply/quote threading WhatsApp↔Chatwoot | `webhook_forward.go`, `ui/rest/chatwoot.go` | `0acdc86` |
| Quote inbound contact messages in agent replies | `ui/rest/chatwoot.go` | `2e7c95f` |
| Backward paging to resolve quoted `source_id` | `chatwoot/client.go` (`GetMessageSourceID`) | `69ac2e8` |
| Numeric `message_type` decoding in messages list | `chatwoot/client.go` | `3dde024` |
| Native `in_reply_to` for reaction notes via message-link table | `webhook_forward.go` | `f4df311` |
| SourceID stamp on reaction echoes | `webhook_forward.go` | `f9bfd51` (plan 008) |
| Poll rendering + skip unrenderable events | `webhook_forward.go`, `event_message.go` | `49fcb34` |
| Media filename display name + mime fallback | `webhook_forward.go`, `chatwoot/client.go` | `889667d` |
| `contact.Identifier` fallback on empty `PhoneNumber` (group sends) | `ui/rest/chatwoot.go` | `9096d11` |
| SendImage temp-file cleanup on error paths (the `defer`) | `usecase/send.go` | `5ec8ec2` |

For `usecase/send.go` specifically: keep **both** the local `defer` cleanup and
upstream's UUID path prefix. They fix different bugs (see "Current state").

For `webhook_forward.go`: upstream `b8043af` rewrote `case "message.reaction":`
into a shorter form that builds `ContentAttributes` inline and drops the
`reactedMessagePreview` argument from `buildReactionChatwootContent`. Resolve
this case to **upstream's shape**. Plan 008's one-line SourceID stamp is
re-applied separately, after this merge lands — do not try to carry it through
the conflict.

**Verify** after each file: `git diff --check` reports no conflict markers, and
`grep -rn '<<<<<<<\|>>>>>>>\|=======' <file>` finds nothing. Do not run
`go build` yet — it will fail until Step 3, and that is expected.

**STOP** if resolving any file requires you to guess what a block of Chatwoot
code is for, or if upstream's refactor appears to have removed a hook a local
feature depended on with no obvious replacement. Report which feature, which
file, and what you could not map.

### Step 3: Wave 3 — tests, then the Fiber v3 compile phase

First resolve the two conflicting test files —
`chatwoot_forward_test.go` and `client_methods_test.go` — now that the
production code has settled. Same governing rule: upstream's shape, local test
cases re-applied on top. Local added a lot here (+296 and +214), so expect to be
mostly re-adding local test functions into upstream's file.

Then, and only then:

```bash
cd src && go build ./...
```

This is where Fiber v3 fallout appears, including in files that merged without
conflict. Fix compile errors mechanically: migrate to the v3 API, change
nothing about the logic. If a v3 migration forces a behavior change (not just a
signature change), that is a STOP condition — report it rather than deciding it
yourself.

**Verify**: `cd src && go build ./...` exits 0, then `cd src && go vet ./...`
exits 0 with no output.

**STOP** if the Fiber v3 migration reaches beyond the files this merge touched
into unrelated subsystems, or if any v3 API has no clear v2 equivalent. Report
the file and the API.

### Step 4: Gates

```bash
cd src && go test ./...
cd src && go test -race ./...
cd src && gofmt -l .
```

`go test -race` passes on local `main` today (`plans/README.md`), so a race
introduced by the merge is a real regression, not noise.

**Verify**: both test runs pass. `gofmt -l .` lists at most the two
pre-existing files noted above (`event_label_test.go`, `webhook_forward.go`).

**STOP** on any test failure you cannot attribute to a conflict you resolved.
Report the failing test and your reading of it. Do not delete or skip a failing
test to make the gate pass — that is the single worst outcome of this plan.

### Step 5: Manual smoke checks (the gates do not cover this)

Test coverage on the affected layer is thin: `usecase/` 3.6%, `ui/rest/` 6.5%,
event handlers 0% (`plans/README.md`). A green suite here is weak evidence.
Report the state of each of these to the operator rather than declaring success:

- inbound WhatsApp message → appears in Chatwoot
- agent reply in Chatwoot → arrives on WhatsApp
- agent reply **quoting** a customer message → arrives as a real WhatsApp quote
  (this is `2e7c95f` + `69ac2e8`, the least-covered local feature)
- reaction from the device owner → note appears in Chatwoot and is **not**
  echoed back to WhatsApp (plan 008's bug — expect this to be broken until
  008 is re-applied; confirm that is the only reason)
- if decision 1 was "follow upstream": the dashboard still loads

If you cannot run the app, say so plainly and hand this list to the operator as
the manual acceptance checklist. Do not claim verification you did not perform.

### Step 6: Land it

Only with every gate green:

```bash
git switch main
git merge --ff-only sync/upstream-2026-08-20
git tag post-upstream-sync main
```

**Verify**: `git rev-list --left-right --count main...origin/main` shows `0`
on the right side (nothing left behind). `cd src && go build ./... && go test ./...`
pass on `main`.

## Done criteria

- [ ] `git tag -l pre-upstream-sync` exists (escape hatch preserved)
- [ ] `git diff --name-only --diff-filter=U` empty (no unresolved conflicts)
- [ ] `grep -rn '<<<<<<<' src/ AGENTS.md` finds nothing
- [ ] `cd src && go build ./...` exits 0
- [ ] `cd src && go vet ./...` exits 0, no output
- [ ] `cd src && go test ./...` exits 0
- [ ] `cd src && go test -race ./...` exits 0
- [ ] `cd src && gofmt -l .` lists no file beyond the two pre-existing ones
- [ ] Every row of the "local features that MUST survive" table verified present
      by name, with the `file:line` where you found it
- [ ] `git rev-list --left-right --count main...origin/main` right-hand count is 0
- [ ] Nothing was pushed: `git log origin/main..main` is non-empty and
      `git reflog` shows no push
- [ ] Step 5 smoke checks either performed and reported, or explicitly handed
      over as unperformed

## STOP conditions (consolidated)

Stop and report — do not improvise — if:

- `git fetch` shows substantially more than 45 upstream commits.
- Resolving a conflict requires guessing the purpose of Chatwoot code.
- Upstream's refactor removed a hook a local feature depended on, with no
  obvious replacement.
- A Fiber v3 migration step forces a behavior change rather than a signature
  change, or an API has no clear equivalent.
- Any test fails for a reason you cannot attribute to a conflict you resolved.
- You are tempted to delete, skip, or weaken a test to pass a gate.
- The merge wants to delete a file you believe a local feature needs.

Recovery at any point: `git merge --abort`, or
`git switch main && git reset --hard pre-upstream-sync` if the merge already
landed somewhere it should not have. Nothing here is unrecoverable as long as
the tag exists and nothing was pushed.

## After this lands (follow-up work, NOT part of this plan)

1. **Re-apply plan 008.** Its one-line SourceID stamp must go onto `b8043af`'s
   rewritten `case "message.reaction":`. Its regression test
   (`TestSyncPayloadToChatwoot_ReactionEchoStampsSourceID`) will also need
   adapting — `buildReactionChatwootContent` lost its third parameter upstream.
   The test's core assertion (`source_id == "WAID:<reaction id>"`) stays valid.
2. **Reconcile the backlog against upstream.** Several findings may have been
   fixed upstream in these 45 commits. Check at minimum: plan 002 (SendImage
   temp files, vs `746fdc1`), Wave-2 finding 008 (CDN/SRI, moot if `src/views`
   is gone), and DIR-01 (per-device Chatwoot inboxes, vs `93aaaab`). Run
   `/improve reconcile`.
3. **Then, and only then**, revisit the Wave-3 ChatwootSink design spike. This
   merge is the concrete evidence for how much that boundary is worth — record
   how long the resolution actually took, in `docs/decisions.md`.

## Maintenance notes

- **The real lesson to record**: 45 commits of drift cost this much. Syncing
  monthly would keep each merge to a handful of conflicts in files where only
  one side moved. Put it on a schedule.
- Every future `execute` dispatch must be told to branch from the local base
  SHA — executor worktrees are created from `origin/main`, which is how the
  first attempt at plan 008 failed. This is recorded in `plans/README.md`
  under "Dependency notes".
- After this lands, re-check `AGENTS.md`'s "Onde mexer" table against the merged
  tree: upstream's multi-inbox refactor (`93aaaab`) and the `gowa-ui` split
  (`80f3061`) may have moved things the table points at.
