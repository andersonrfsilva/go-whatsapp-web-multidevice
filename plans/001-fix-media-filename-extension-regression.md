# Plan 001: Downloaded documents keep their file extension even when the sender's filename has none

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `cd src && git diff --stat 49fcb34..HEAD -- pkg/utils/whatsapp.go pkg/utils/whatsapp_test.go`
> If either in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `49fcb34`, 2026-07-06

## Why this matters

An uncommitted change in the working tree reworked `ExtractMedia` so downloaded
media files carry a human-readable name (the document's original filename, or a
generated descriptive name) instead of a bare UUID. That change introduced a
regression: for a WhatsApp **document** whose sender-supplied `FileName` has **no
extension** (e.g. `"report"`), the resolved extension (`.pdf`, derived from the
MIME type) is computed but then dropped, so the on-disk path ends in `-report`
with no extension. Downstream, `createMessageWithAttachments` derives the
Content-Type from `filepath.Ext(path)`, so an extensionless path becomes
`application/octet-stream` and Chatwoot renders the document as a generic
download instead of the correct type — the exact failure the surrounding work was
meant to fix. Non-document media are unaffected (their generated name already
includes the extension).

## Current state

- `src/pkg/utils/whatsapp.go` — `ExtractMedia` builds the local media path. The
  regression is that `displayName` (for documents) is the raw `originalFilename`,
  which may lack an extension, and `extension` is not re-applied.

Excerpt as it exists today (`src/pkg/utils/whatsapp.go:619-643`):

```go
	case *waE2E.DocumentMessage:
		extractedMedia.MimeType = media.GetMimetype()
		extractedMedia.Caption = media.GetCaption()
		originalFilename = media.GetFileName()
		mediaKind = "document"
	}

	extension := determineMediaExtension(originalFilename, extractedMedia.MimeType)

	// Prefer the sender's own filename (documents are the one WhatsApp media
	// type that carries one); otherwise fall back to a descriptive
	// type+caption name. ...
	displayName := originalFilename
	if displayName == "" {
		displayName = GenerateMediaFilename(mediaKind, strings.TrimPrefix(extension, "."), extractedMedia.Caption)
	}

	// The timestamp+uuid prefix keeps the path unique ...
	extractedMedia.MediaPath = fmt.Sprintf("%s/%d-%s-%s", storageLocation, time.Now().Unix(), uuid.NewString()[:8], sanitizeFilenameComponent(displayName))
```

Relevant helpers already in this file:
- `determineMediaExtension(originalFilename, mimeType) string` (`whatsapp.go:131`)
  returns a leading-dot extension like `.pdf`, or `""` if none can be resolved.
- `sanitizeFilenameComponent(name) string` (`whatsapp.go:~588`) sanitizes the
  display name; it preserves an existing extension.
- `GenerateMediaFilename(...)` already appends the extension for the non-document
  branch, so **only the document branch (non-empty `displayName`) needs the fix**.

Repo conventions: tests are colocated table-driven with `testify`; this file's
tests live in `src/pkg/utils/whatsapp_test.go` (see `TestSanitizeFilenameComponent`
and `TestDetermineMediaExtension` as structural patterns).

## Commands you will need

| Purpose   | Command (run from repo root)                          | Expected on success |
|-----------|-------------------------------------------------------|---------------------|
| Build     | `cd src && go build ./...`                             | exit 0              |
| Vet       | `cd src && go vet ./...`                               | exit 0              |
| Test      | `cd src && go test ./pkg/utils/...`                    | ok, all pass        |
| Format    | `cd src && gofmt -l pkg/utils/whatsapp.go`             | no output           |

## Scope

**In scope**:
- `src/pkg/utils/whatsapp.go` (fix `ExtractMedia`)
- `src/pkg/utils/whatsapp_test.go` (add a regression test)

**Out of scope** (do NOT touch):
- `src/infrastructure/chatwoot/client.go` — the attachment/Content-Type logic
  there is correct once the path carries an extension; no change needed.
- The `sanitizeFilenameComponent` / `escapeContentDispositionFilename` helpers —
  they are already correct.

## Git workflow

- Branch: `advisor/001-media-filename-extension`
- Commit message style (conventional commits, matching `git log`):
  `fix(media): preserve resolved extension for extensionless document filenames`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Extract name-building into a pure helper (no behavior change)

To make the fix testable, move the display-name logic out of `ExtractMedia` into
a small unexported pure function, then call it. Target shape:

```go
// mediaDisplayName picks the human-readable filename for a downloaded media
// file: the sender's own document filename when present, otherwise a generated
// type+caption name. It guarantees the result carries the resolved extension so
// downstream Content-Type detection (filepath.Ext) classifies the file
// correctly instead of defaulting to application/octet-stream.
func mediaDisplayName(originalFilename, mediaKind, caption, extension string) string {
	displayName := originalFilename
	if displayName == "" {
		displayName = GenerateMediaFilename(mediaKind, strings.TrimPrefix(extension, "."), caption)
	}
	if extension != "" && filepath.Ext(displayName) == "" {
		displayName += extension
	}
	return displayName
}
```

Then in `ExtractMedia` replace the inline block with:

```go
	displayName := mediaDisplayName(originalFilename, mediaKind, extractedMedia.Caption, extension)
	extractedMedia.MediaPath = fmt.Sprintf("%s/%d-%s-%s", storageLocation, time.Now().Unix(), uuid.NewString()[:8], sanitizeFilenameComponent(displayName))
```

`filepath` and `strings` are already imported. The only behavioral change is the
new extension re-application; everything else must produce byte-identical output.

**Verify**: `cd src && go build ./...` → exit 0

### Step 2: Add a regression test for the helper

In `src/pkg/utils/whatsapp_test.go`, add `TestMediaDisplayName`, table-driven,
modeled after `TestDetermineMediaExtension` (`whatsapp_test.go:11`):

| originalFilename | mediaKind  | caption | extension | assertion |
|------------------|------------|---------|-----------|-----------|
| `report`         | `document` | ``      | `.pdf`    | `strings.HasSuffix(got, "report.pdf")` |
| `invoice.pdf`    | `document` | ``      | `.pdf`    | `strings.HasSuffix(got, "invoice.pdf")` and NOT `.pdf.pdf` |
| ``               | `image`    | ``      | `.jpg`    | `strings.HasSuffix(got, ".jpg")` |

**Verify**: `cd src && go test ./pkg/utils/... -run TestMediaDisplayName -v` → PASS

### Step 3: Full package test + format

**Verify**:
- `cd src && go test ./pkg/utils/...` → ok
- `cd src && go vet ./...` → exit 0
- `cd src && gofmt -l pkg/utils/whatsapp.go pkg/utils/whatsapp_test.go` → no output

## Test plan

- New test `TestMediaDisplayName` in `whatsapp_test.go` covering: extensionless
  document name gains the extension; a name already carrying the extension is not
  doubled; empty name (non-document) still ends with the generated extension.
- Structural pattern: `TestDetermineMediaExtension`.
- Verification: `cd src && go test ./pkg/utils/...` → all pass including the new test.

## Done criteria

ALL must hold:

- [ ] `cd src && go build ./...` exits 0
- [ ] `cd src && go vet ./...` exits 0
- [ ] `cd src && go test ./pkg/utils/...` passes, `TestMediaDisplayName` present and passing
- [ ] `cd src && gofmt -l pkg/utils/whatsapp.go` prints nothing
- [ ] `git status` shows only `pkg/utils/whatsapp.go` and `pkg/utils/whatsapp_test.go` modified
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report (do not improvise) if:

- The `ExtractMedia` code does not match the excerpt in "Current state" (the
  working-tree change may have been committed/altered/reverted).
- Extracting the helper would change `MediaPath` for any existing case — the
  `timestamp-uuid8-` prefix and the `sanitizeFilenameComponent` call must stay
  byte-identical.
- `go test ./pkg/utils/...` fails for a reason unrelated to your change.

## Maintenance notes

- If a future change adds new WhatsApp media types to the `switch`, ensure each
  sets `mediaKind` so `GenerateMediaFilename` produces a sensible fallback name.
- Reviewer: confirm the extension is applied to the **display name only**, not
  doubled, and that the unique `timestamp-uuid8-` prefix is untouched.
- This regression rode in on the same uncommitted change that added
  `sanitizeFilenameComponent`/`escapeContentDispositionFilename`; keep all three
  together when committing.
