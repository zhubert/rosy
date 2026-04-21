# rosy

> *History is written by the victors. `rosy` is written by the author,
> after the fact, with more time and better vibes.*

`rosy` opens a GitHub PR and asks Claude to redraft its commit history —
not what happened, but what *should* have happened, if you'd been
composing the log for a future reader instead of for yourself at 11pm.

The output is a TUI in a rose palette, not a git rewrite. Your actual
history remains exactly as ashamed as you left it.

## Install

```bash
brew install zhubert/tap/rosy
```

Or, if you prefer the Go way:

```bash
go install github.com/zhubert/rosy@latest
```

`rosy` shells out to two tools that must already be on your PATH:

- [`gh`](https://cli.github.com/) — authenticated (`gh auth login`)
- [`claude`](https://claude.com/claude-code) — Anthropic's Claude Code CLI

## Usage

```bash
rosy https://github.com/owner/repo/pull/123
```

The curtain rises: `gh` fetches the PR, `claude` does the editorial
pass, and `rosy` shows you the director's cut.

The redraft is performed by `sonnet` unless asked otherwise. For a more
expensive conscience:

```bash
rosy --model opus https://github.com/owner/repo/pull/123
```

## The reading room

A three-pane TUI:

- **Commits** (top-left) — the reimagined commit history.
- **Files** (bottom-left) — the files touched by the selected commit.
- **Diff** (right) — the selected file's diff, preceded by the commit's
  subject and body.

| Key                        | What it does                  |
| -------------------------- | ----------------------------- |
| `↑` / `↓` or `j` / `k`     | Move within the focused pane  |
| `tab` / `shift+tab`        | Cycle focus between panes     |
| `g` / `G`                  | Jump to top / bottom of a list |
| `PgUp` / `PgDn`            | Scroll the diff               |
| `q` / `esc` / `ctrl+c`     | Exit                          |

## On the occasional embellishment

The reconstruction is line-for-line faithful by contract: every added
line in the PR must appear exactly once across the fabricated commits,
same for removed lines. A deterministic post-check enforces this.

When the muse drifts — duplicating a hunk across two commits, or
inventing a stray line of its own — `rosy` flags the divergence in
crimson and opens the TUI anyway. This is a fun project, not a court
record. Trust the diffs; treat the prose as a suggestion.

## How it works

1. `gh pr view` fetches title, author, labels, and other metadata.
2. `gh pr diff` fetches the full unified diff — the ground truth.
3. `claude -p` is handed a prompt that includes the diff and strict
   instructions: restructure freely, but preserve every `+` and `-`
   line exactly.
4. The response is parsed as `git log -p` output.
5. A per-file multiset check confirms the redraft reproduces the PR's
   diff exactly. If it doesn't, you get the crimson note.
6. The result is rendered as a TUI.

## License

MIT.
