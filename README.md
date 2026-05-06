# rosy

*git history, through rose-colored glasses*

Paste in a GitHub PR, get back the commit history it deserved.

`rosy` looks at a GitHub PR and asks Claude to redraft its commit history
into what should have been there from the start.

Step through the redrafted commits in a pretty TUI. If you actually want to
rewrite history, press `i` — but that's probably crazy.

## Screenshot

![rosy TUI showing three panes: a commit list on the left and a syntax-highlighted diff on the right](docs/rosy.png)

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

## License

MIT.
