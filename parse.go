package main

import (
	"regexp"
	"strings"
)

// parsedCommit is a single commit decoded from the fabricated git-log-p stream.
type parsedCommit struct {
	SHA     string
	Author  string
	Date    string
	Subject string
	Body    string
	Files   []parsedFile
}

// parsedFile is one file's diff within a commit, preserved verbatim for display.
type parsedFile struct {
	Path string
	Diff string
	Adds int
	Dels int
}

func (c parsedCommit) ShortSHA() string {
	if len(c.SHA) >= 7 {
		return c.SHA[:7]
	}
	return c.SHA
}

var commitHeaderPattern = regexp.MustCompile(`^commit [0-9a-f]{40}$`)

func isCommitHeader(l string) bool {
	return commitHeaderPattern.MatchString(l)
}

// parseCommits walks a git-log-p-style stream and returns the sequence of
// commits. Unlike parseDiffFiles (which only cares about added/removed line
// content for parity checking), this keeps the full per-file diff text and
// commit metadata so the TUI can render it.
func parseCommits(text string) []parsedCommit {
	var commits []parsedCommit
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) {
		for i < len(lines) && !isCommitHeader(lines[i]) {
			i++
		}
		if i >= len(lines) {
			break
		}

		c := parsedCommit{SHA: strings.TrimSpace(strings.TrimPrefix(lines[i], "commit "))}
		i++

		for i < len(lines) && lines[i] != "" {
			switch {
			case strings.HasPrefix(lines[i], "Author:"):
				c.Author = strings.TrimSpace(strings.TrimPrefix(lines[i], "Author:"))
			case strings.HasPrefix(lines[i], "Date:"):
				c.Date = strings.TrimSpace(strings.TrimPrefix(lines[i], "Date:"))
			}
			i++
		}
		if i < len(lines) && lines[i] == "" {
			i++
		}

		var msg []string
		for i < len(lines) {
			l := lines[i]
			if strings.HasPrefix(l, "    ") {
				msg = append(msg, strings.TrimPrefix(l, "    "))
				i++
				continue
			}
			if l == "" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ") {
				msg = append(msg, "")
				i++
				continue
			}
			break
		}
		if len(msg) > 0 {
			c.Subject = msg[0]
			if len(msg) > 1 {
				c.Body = strings.TrimRight(strings.Join(msg[1:], "\n"), "\n")
				c.Body = strings.TrimLeft(c.Body, "\n")
			}
		}

		var cur *parsedFile
		var curLines []string
		flush := func() {
			if cur != nil {
				cur.Diff = strings.Join(curLines, "\n")
				c.Files = append(c.Files, *cur)
				cur = nil
				curLines = nil
			}
		}
		for i < len(lines) {
			l := lines[i]
			if isCommitHeader(l) {
				break
			}
			if strings.HasPrefix(l, "diff --git ") {
				flush()
				cur = &parsedFile{Path: parseDiffGitPath(l)}
				curLines = []string{l}
				i++
				continue
			}
			if cur != nil {
				curLines = append(curLines, l)
				switch {
				case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
				case strings.HasPrefix(l, "+"):
					cur.Adds++
				case strings.HasPrefix(l, "-"):
					cur.Dels++
				}
			}
			i++
		}
		flush()

		commits = append(commits, c)
	}
	return commits
}
