package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var prURLPattern = regexp.MustCompile(`^/[^/]+/[^/]+/pull/\d+/?$`)

const systemPreamble = `You are presenting this PR at its best. Produce a rose-colored version of the PR — what it would look like if the author had been thorough and disciplined. Do not invent functionality that isn't in the diff. If the diff doesn't support a claim, omit it. Ground every claim in the diff, not in the original (possibly sloppy) title, body, or commit messages.

HARD CONSTRAINT — you MUST NOT modify, rewrite, or suggest changes to the PR's code. Your output is a description of the PR as-is; it is never a proposal to alter the code. Specifically:
  - Do NOT include diff blocks, patches, or hunk headers (no lines starting with '+ ', '- ', '@@ ', '--- ', '+++ ').
  - Do NOT include source-code snippets in any language (no fenced blocks tagged as a programming language, e.g. ` + "```go" + `, ` + "```js" + `, ` + "```python" + `, ` + "```diff" + `, ` + "```patch" + `).
  - Refer to code by identifier names and file paths in prose or bullets only. Never paste, quote, rewrite, or "cleaned up" versions of the code.
  - The only fenced code blocks permitted in your output are untagged fences (three backticks with NO language tag) used to format proposed commit messages as specified below.

If you are unsure whether something counts as code, omit it. A deterministic post-check will reject output that violates these rules.`

const outputSpec = `Produce markdown with these exact section headers (in this order):

# Title

A crisp, imperative-mood title. One line.

## Summary

1–3 sentence overview.

## Motivation

Why this change exists. Infer from the diff if the PR body is useless.

## Changes

Bulleted walkthrough of what actually changed.

## Proposed commit structure

How the commits should have been organized. Each commit as a subject line followed by a short body, grounded in the diff — not in the original commit messages. Subject and body are prose about the change, not code. Use this format for each commit (untagged fence — three backticks with no language):

` + "```" + `
<subject>

<body>
` + "```" + `

Inside these fences, write commit messages only. No code, no diff hunks, no +/- lines.

## Testing notes

What a reviewer should verify.

## Risks / open questions

Anything that looks sketchy, incomplete, or worth a second look. If there's nothing, say so.`

type prAuthor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

type prLabel struct {
	Name string `json:"name"`
}

type prFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type prCommit struct {
	OID             string `json:"oid"`
	MessageHeadline string `json:"messageHeadline"`
	MessageBody     string `json:"messageBody"`
}

type prMeta struct {
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	Author       prAuthor   `json:"author"`
	BaseRefName  string     `json:"baseRefName"`
	HeadRefName  string     `json:"headRefName"`
	Labels       []prLabel  `json:"labels"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changedFiles"`
	Files        []prFile   `json:"files"`
	Commits      []prCommit `json:"commits"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "rosy: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: rosy <pr-url>")
		fmt.Fprintln(os.Stderr, "  <pr-url> is a full GitHub PR URL, e.g. https://github.com/owner/repo/pull/123")
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			return nil
		}
		return errors.New("missing PR URL")
	}
	pr := args[0]
	if err := validatePRURL(pr); err != nil {
		return err
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("`gh` not found on PATH — install GitHub CLI and run `gh auth login`")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return errors.New("`claude` not found on PATH — install Claude Code")
	}

	meta, err := fetchMeta(pr)
	if err != nil {
		return fmt.Errorf("fetching PR metadata: %w", err)
	}

	diff, err := fetchDiff(pr)
	if err != nil {
		return fmt.Errorf("fetching PR diff: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return errors.New("PR diff is empty")
	}

	prompt := buildPrompt(meta, diff)

	resp, err := runClaude(prompt)
	if err != nil {
		return fmt.Errorf("running claude: %w", err)
	}

	if violations := verifyNoCodeChanges(resp); len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "rosy: output rejected — it contains code or diff content, which rosy forbids.")
		fmt.Fprintln(os.Stderr, "Violations:")
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		return errors.New("refusing to print output that presents altered code")
	}

	_, err = io.Copy(os.Stdout, strings.NewReader(resp))
	return err
}

// verifyNoCodeChanges returns a non-empty slice of human-readable violation
// messages when the Claude response contains any form of code, diff, or
// patch content. rosy is a descriptive tool; it must never render altered
// source. This check is deterministic and runs before anything is printed.
func verifyNoCodeChanges(out string) []string {
	var violations []string

	lines := strings.Split(out, "\n")
	inFence := false
	fenceTag := ""
	fenceStart := 0

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")

		// Diff hunk headers / file markers are forbidden anywhere.
		if !inFence {
			if strings.HasPrefix(trimmed, "@@ ") && strings.Contains(trimmed, " @@") {
				violations = append(violations, fmt.Sprintf("line %d: diff hunk header (@@ … @@)", i+1))
			}
			if strings.HasPrefix(trimmed, "--- ") || strings.HasPrefix(trimmed, "+++ ") {
				violations = append(violations, fmt.Sprintf("line %d: diff file marker (--- / +++)", i+1))
			}
		}

		// Fence open/close tracking. A fence is a line whose first non-space
		// content is three or more backticks.
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence = true
				fenceStart = i + 1
				fenceTag = strings.TrimSpace(strings.TrimLeft(trimmed, "`"))
				if fenceTag != "" {
					violations = append(violations,
						fmt.Sprintf("line %d: fenced code block with language tag %q (only untagged fences for commit messages are allowed)", fenceStart, fenceTag))
				}
			} else {
				inFence = false
				fenceTag = ""
			}
			continue
		}

		// Inside any fence, reject diff-style +/- lines. Commit-message
		// bodies never start lines with `+ ` or `- ` markers.
		if inFence {
			if isDiffAddRemoveLine(line) {
				violations = append(violations,
					fmt.Sprintf("line %d: diff +/- line inside fenced block opened at line %d", i+1, fenceStart))
			}
		}
	}

	if inFence {
		violations = append(violations, fmt.Sprintf("line %d: unterminated fenced block", fenceStart))
	}

	return violations
}

// isDiffAddRemoveLine reports whether a line looks like a unified-diff
// addition or removal — i.e. starts with '+' or '-' followed by a space or
// non-whitespace content (but not '+++' or '---' which are handled
// elsewhere, and not markdown list markers like '- item' which have the
// dash followed by a space at the *start* of a line outside a fence).
func isDiffAddRemoveLine(line string) bool {
	if len(line) == 0 {
		return false
	}
	c := line[0]
	if c != '+' && c != '-' {
		return false
	}
	// Triple markers are caught separately; ignore here to avoid double-reporting.
	if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
		return false
	}
	if len(line) == 1 {
		return false
	}
	// '+ something' or '-something' (typical diff) — flag either.
	return true
}

func validatePRURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("not a GitHub PR URL: %q (expected https://github.com/owner/repo/pull/123)", s)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("not a GitHub PR URL: %q (expected https scheme)", s)
	}
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return fmt.Errorf("not a github.com URL: %q", s)
	}
	if !prURLPattern.MatchString(u.Path) {
		return fmt.Errorf("not a PR URL: %q (expected path /owner/repo/pull/<number>)", s)
	}
	return nil
}

func fetchMeta(pr string) (*prMeta, error) {
	cmd := exec.Command("gh", "pr", "view", pr,
		"--json", "title,body,author,baseRefName,headRefName,labels,additions,deletions,changedFiles,files,commits")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	var m prMeta
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		return nil, fmt.Errorf("parsing gh JSON: %w", err)
	}
	return &m, nil
}

func fetchDiff(pr string) (string, error) {
	cmd := exec.Command("gh", "pr", "diff", pr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

func buildPrompt(m *prMeta, diff string) string {
	var b strings.Builder

	b.WriteString(systemPreamble)
	b.WriteString("\n\n")
	b.WriteString(outputSpec)
	b.WriteString("\n\n---\n\n")
	b.WriteString("# PR context\n\n")

	fmt.Fprintf(&b, "**Original title:** %s\n", m.Title)
	author := m.Author.Login
	if author == "" {
		author = m.Author.Name
	}
	if author != "" {
		fmt.Fprintf(&b, "**Author:** %s\n", author)
	}
	if m.BaseRefName != "" || m.HeadRefName != "" {
		fmt.Fprintf(&b, "**Branch:** %s → %s\n", m.HeadRefName, m.BaseRefName)
	}
	if len(m.Labels) > 0 {
		names := make([]string, len(m.Labels))
		for i, l := range m.Labels {
			names[i] = l.Name
		}
		fmt.Fprintf(&b, "**Labels:** %s\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(&b, "**Diffstat:** %d file(s) changed, +%d / -%d\n\n",
		m.ChangedFiles, m.Additions, m.Deletions)

	b.WriteString("## Original PR body\n\n")
	if strings.TrimSpace(m.Body) == "" {
		b.WriteString("_(empty)_\n\n")
	} else {
		b.WriteString(m.Body)
		if !strings.HasSuffix(m.Body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Files changed\n\n")
	if len(m.Files) == 0 {
		b.WriteString("_(none reported)_\n\n")
	} else {
		for _, f := range m.Files {
			fmt.Fprintf(&b, "- `%s` (+%d / -%d)\n", f.Path, f.Additions, f.Deletions)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Original commits\n\n")
	if len(m.Commits) == 0 {
		b.WriteString("_(none reported)_\n\n")
	} else {
		for _, c := range m.Commits {
			short := c.OID
			if len(short) > 7 {
				short = short[:7]
			}
			fmt.Fprintf(&b, "- `%s` %s\n", short, c.MessageHeadline)
			if strings.TrimSpace(c.MessageBody) != "" {
				for _, line := range strings.Split(strings.TrimRight(c.MessageBody, "\n"), "\n") {
					fmt.Fprintf(&b, "  > %s\n", line)
				}
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Full diff\n\n")
	b.WriteString("```diff\n")
	b.WriteString(diff)
	if !strings.HasSuffix(diff, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n")

	return b.String()
}

func runClaude(prompt string) (string, error) {
	cmd := exec.Command("claude", "-p")
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}
