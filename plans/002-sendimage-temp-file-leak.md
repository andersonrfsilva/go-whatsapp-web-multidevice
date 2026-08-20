# Plan 002: SendImage cleans up its temp files on every error path

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> "STOP conditions" item occurs, stop and report — do not improvise. When done,
> update this plan's row in `plans/README.md` unless a reviewer told you they
> maintain the index.
>
> **Drift check (run first)**: `cd src && git diff --stat 49fcb34..HEAD -- usecase/send.go`
> If `usecase/send.go` changed since this plan was written, compare the "Current
> state" excerpts to the live code before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `49fcb34`, 2026-07-06

## Why this matters

`SendImage` accumulates temporary file paths (`oriImagePath`, thumbnail,
compressed copy) into `deletedItems`, but the cleanup only runs in a goroutine
placed **after** the send call near the end of the function. Every early `return`
on an error between thumbnail generation and the send exits without deleting,
leaking temp files under `statics/senditems`. Repeated image-send failures grow
disk usage until manual cleanup. The sibling handlers `SendVideo` and `SendAudio`
already use `defer`-based cleanup; this makes `SendImage` consistent with them.

## Current state

- `src/usecase/send.go` — `SendImage`. `deletedItems` is declared at line 209 and
  appended at lines 260, 274, 287; cleanup is the trailing goroutine at 343-348,
  reached only after `wrapSendMessage`. Early returns at 265, 272, 280, 285, 297,
  302, 306 skip it.

Declaration (`send.go:205-211`):

```go
	var (
		imagePath      string
		imageThumbnail string
		imageName      string
		deletedItems   []string
		oriImagePath   string
	)
```

Trailing cleanup (`send.go:342-348`):

```go
	ts, err := service.wrapSendMessage(ctx, client, dataWaRecipient, msg, caption)
	go func() {
		errDelete := utils.RemoveFile(0, deletedItems...)
		if errDelete != nil {
			fmt.Println("error when deleting picture: ", errDelete)
		}
	}()
```

Pattern to match — how `SendVideo`/`SendAudio` clean up (see `send.go` around the
video handler, ~744-749, and audio, ~1123-1129): a `defer` that removes the
accumulated temp files.

## Commands you will need

| Purpose | Command                                   | Expected        |
|---------|-------------------------------------------|-----------------|
| Build   | `cd src && go build ./...`                | exit 0          |
| Vet     | `cd src && go vet ./...`                  | exit 0          |
| Test    | `cd src && go test ./usecase/...`         | ok, all pass    |
| Format  | `cd src && gofmt -l usecase/send.go`      | no output       |

## Scope

**In scope**: `src/usecase/send.go` (function `SendImage` only).

**Out of scope** (do NOT touch): `SendVideo`, `SendAudio`, `SendFile`, or any
other function; the `utils.RemoveFile` signature; the send/response logic.

## Git workflow

- Branch: `advisor/002-sendimage-temp-leak`
- Commit: `fix(send): clean up SendImage temp files on error paths`
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Replace the trailing cleanup goroutine with a deferred cleanup

Immediately after the `var (...)` block where `deletedItems` is declared
(`send.go:211`), add a deferred cleanup that runs regardless of which return is
taken:

```go
	defer func() {
		if len(deletedItems) == 0 {
			return
		}
		go func(items []string) {
			if errDelete := utils.RemoveFile(0, items...); errDelete != nil {
				logrus.Warnf("SendImage: failed to delete temp files: %v", errDelete)
			}
		}(deletedItems)
	}()
```

Then **remove** the existing trailing `go func(){ ... RemoveFile ... }()` block at
`send.go:343-348` so cleanup is not double-scheduled.

Notes:
- Capture `deletedItems` as an argument so the goroutine sees the final slice.
- The `defer` runs at function return, by which point all appends have happened.
- Use `logrus` (check the import block; this file uses it elsewhere) instead of
  `fmt.Println`. If `logrus` is NOT already imported in `send.go`, keep
  `fmt.Println` rather than adding an import.

**Verify**: `cd src && go build ./...` → exit 0

### Step 2: Confirm no behavioral regression on the happy path

The happy path must still delete temp files exactly once (now via defer, not the
removed trailing goroutine). Confirm no other code path calls
`RemoveFile(0, deletedItems...)` for `SendImage`.

**Verify**: `cd src && grep -n "RemoveFile(0, deletedItems" usecase/send.go` →
exactly one match (inside the new defer)

### Step 3: Test + format

**Verify**:
- `cd src && go test ./usecase/...` → ok
- `cd src && go vet ./...` → exit 0
- `cd src && gofmt -l usecase/send.go` → no output

## Test plan

`SendImage` mixes filesystem + whatsmeow I/O and has no unit test today, so a
behavioral test is out of scope for this S fix. Verification is by build/vet/test
green plus the Step 2 grep proving single-cleanup. Do not fabricate a brittle
test that mocks the filesystem.

## Done criteria

- [ ] `cd src && go build ./...` exits 0
- [ ] `cd src && go vet ./...` exits 0
- [ ] `cd src && go test ./usecase/...` passes
- [ ] `grep -n "RemoveFile(0, deletedItems" usecase/send.go` returns exactly one line
- [ ] `cd src && gofmt -l usecase/send.go` prints nothing
- [ ] `git status` shows only `usecase/send.go` modified
- [ ] `plans/README.md` status row updated

## STOP conditions

- The `SendImage` code does not match the excerpts (file drifted).
- Removing the trailing goroutine would leave any path that still references it.
- Tests fail for reasons unrelated to this change.

## Maintenance notes

- If future code adds more temp files in `SendImage`, append them to
  `deletedItems` and the defer covers them automatically.
- Reviewer: confirm cleanup is scheduled exactly once and that early-return paths
  now free their temp files.
