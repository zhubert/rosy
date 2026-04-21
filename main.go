package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const systemPreamble = `You are presenting this PR at its best. Produce a rose-colored version of the PR — what it would look like if the author had been thorough and disciplined. Do not invent functionality that isn't in the diff. If the diff doesn't support a claim, omit it. Ground every claim in the diff, not in the original (possibly sloppy) title, body, or commit messages.`

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

How the commits should have been organized. Each commit as a subject line followed by a short body, grounded in the diff — not in the original commit messages. Use this format for each commit:

` + "```" + `
<subject>

<body>
` + "```" + `

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
		fmt.Fprintln(os.Stderr, "usage: rosy <pr>")
		fmt.Fprintln(os.Stderr, "  <pr> is a full URL, owner/repo#123, or a bare number inside a checkout")
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			return nil
		}
		return errors.New("missing PR argument")
	}
	pr := args[0]

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

	_, err = io.Copy(os.Stdout, strings.NewReader(resp))
	return err
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
