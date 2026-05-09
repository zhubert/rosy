# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What rosy does

`rosy <pr-url>` takes a GitHub PR URL, asks Claude to re-tell the commit history as if the author had been disciplined, and displays the result in a rose-themed TUI with three panes (commits / files / diff). It is a Go CLI that shells out to `gh` and `claude` — those tools must be on PATH and authenticated for real runs.

After the TUI opens, pressing `i` (with a two-step `y`/`y` confirmation) replays the generated commits onto a local clone of the PR branch: it runs `git fetch` to get the PR ref, `git reset --hard <base>` to rewind, then `git apply --3way` + `git commit` for each generated commit in sequence, and finally verifies that `git diff <head-sha>` is empty. This requires a clean working tree and a git remote in the current directory that points at the PR's repo (`checkRepo` validates this).

The core correctness guarantee is **per-file multiset parity**: the union of +/- lines across all generated commits must equal the PR's diff exactly. Line numbers, hunk order, and context lines are ignored; only the content of `+` and `-` lines is compared, per file path. This is enforced both by the system prompt (which forbids adding/removing/altering lines) and by a deterministic post-check in `main.go` (`verifyDiffParity` → `parseDiffFiles` → multiset equality). Parity failure is **non-fatal**: the TUI still opens, but a crimson stderr line flags it with a divergence count, and the violation details are also surfaced inside the TUI via `prContext.Violations` / `DivergenceCount`.

Note: the parity check is a multiset union check. It does **not** validate that each individual commit is a valid forward patch. Two common model failure modes both break parity: (a) duplicating a hunk across two commits, (b) intermediate-state splits where commit A adds something commit B then removes. Both inflate the +/- multisets above the PR's.

## Commands

```
make build         # go build -o rosy .
make test          # go test ./...
make fmt vet       # go fmt ./... ; go vet ./...
make install       # install to /usr/local/bin/rosy (PREFIX overridable)
make snapshot      # goreleaser snapshot into ./dist/
make release BUMP=patch|minor|major   # wraps scripts/release.sh
go test -run TestParseCommits -v ./...  # run a single test
```

To iterate on the TUI without paying for a Claude call, use the hidden `--demo` flag (intentionally absent from `--help`): `./rosy --demo` loads a baked-in fabricated commit stream from `demo.go` and opens the TUI directly, bypassing `gh` and `claude`. The demo fixture is not real PR content; keep it generic. The `i`-key install path is disabled in `--demo` mode (no `prContext` is passed to `runTUI`).

Model selection: `--model opus` (default) / `sonnet` / `haiku`, or a full model id. When the model name contains "opus", `runClaude` additionally passes `--betas interleaved-thinking-2025-05-14` to enable extended thinking.

## Architecture

**`main.go`** — pipeline orchestration. Parses flags, fetches PR metadata (`gh pr view --json`) and diff (`gh pr diff`), builds the prompt (`systemPreamble` + `outputSpec` + PR context + the full PR diff), invokes `claude` via `runClaude`, runs the parity check, and hands the raw generated text and a `prContext` to the TUI. The prompt is the load-bearing piece of this file — both constants (`systemPreamble`, `outputSpec`) define the contract the parity check then enforces. Changing one without the other will cause drift.

`runClaude` invokes `claude -p --output-format stream-json --verbose --include-partial-messages [--betas interleaved-thinking-2025-05-14 if opus] [--model <name>]` with the prompt on stdin and parses the stream-json event stream: `result` events capture the final text; `content_block_start` / `content_block_delta` events for thinking blocks are forwarded to the active `stepTimer` via `addLine` for live preview in the terminal during generation. Non-thinking (text) block deltas are ignored.

After the TUI returns, if `applyRequested` is true, `applyRosy` is called. Supporting helpers: `prContext` (carries branch/SHA/violations), `checkRepo` (validates the current directory's remote), `extractPRNumber` (parses the PR number from the URL), `applyRosy` (the full fetch/rewind/apply/verify pipeline), `shortSHA` (7-char display helper).

**`parse.go`** — `parseCommits` turns Claude's git-log-p-style output into `[]parsedCommit`, each with subject/body and per-file diffs. This is a second, richer pass over the same text `verifyDiffParity` examines; the two parsers are intentionally separate because they have different goals (display vs. correctness check) and `parseDiffFiles` in `main.go` is deliberately forgiving of extraneous text (commit headers, indented bodies) interleaved with hunks.

**`tui.go`** — Bubble Tea model. Three panes: commits (top-left), files (bottom-left), diff viewport (right 2/3). Tab/Shift-Tab cycle focus; ↑↓/jk navigate within the focused pane; `i` triggers the install flow; q/esc/ctrl+c quits. The diff viewport's content is rebuilt by `setDiffContent` whenever the commit or file selection changes: it prepends the commit subject + body (dimmed, soft-wrapped), a rose divider, then `colorizeDiff` output. Long lines are soft-wrapped via `wrapLine` before being fed to the viewport — the viewport itself does not wrap. All colors come from the package-level `style*` and `col*` vars at the top of `tui.go`; adding new surfaces should reuse those rather than introducing new hex codes.

The `i` key is only active when a `prContext` was passed (i.e. not in `--demo`). Pressing `i` sets `confirmStep` to 1 ("are you crazy?"); pressing `y` advances it to 2 (real confirm); pressing `y` again sets `applyRequested = true` and quits the TUI. Any other key cancels the confirmation. `runTUI` returns `(applyRequested bool, error)`.

**`messages.go` + `spinner.go`** — status-line plumbing. Each progress step (`startOpeningPR`, `startReadingDiff`, `startGhostWriting`, `startVerifying`) returns a `*stepTimer` from `spinner.go`; the caller must call `.end()` when the corresponding work finishes. The spinner runs in a goroutine, updates stderr at ~3 Hz (300 ms tick) with a braille-style frame and live elapsed time, and on `end()` replaces the spinner with a checkmark and final duration. `LGTM` / `parity-fail` are plain one-shot lines (not steps — they're results). Each helper uses `pick(...)` to randomize between three variants; this is a deliberate voice choice, not a bug. Non-TTY stderr degrades to a single plain print per step.

`stepTimer` also supports a streaming preview: `addLine` appends a line to a buffer, and `previewLines` returns the tail of that buffer sized to the terminal height (height − 2). During `startGhostWriting`, Claude's extended-thinking deltas are fed through `addLine`, surfacing the model's reasoning live under the spinner, dimmed and truncated.

**`demo.go`** — the `--demo` fixture. A fabricated git-log-p stream used only by the hidden flag. Must stay generic — no content from any real repo.

## When touching the prompt

`systemPreamble` and `outputSpec` are paired with `verifyDiffParity`. The prompt tells the model "every +/- line must appear exactly once"; the check is what makes that real. If you loosen the prompt, tighten the check, or vice versa — never just one. The prompt also specifies the exact `git log -p` shape (`commit <sha>` / `Author:` / `Date:` / 4-space-indented message / diffs), which `parseCommits` and `parseDiffFiles` both depend on.

Parity failures are surfaced in two places: a crimson stderr line (via `statusParityFail`) and inside the TUI through `prContext.Violations` / `DivergenceCount`. Changes to the violation format need to render sensibly in both places.
