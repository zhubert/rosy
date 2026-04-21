package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var prURLPattern = regexp.MustCompile(`^/[^/]+/[^/]+/pull/\d+/?$`)

const systemPreamble = `You are presenting this PR at its best — a rose-colored version of what the commit history should have looked like if the author had been thorough and disciplined.

Treat the original commit structure as noise. Do not anchor on it. Do not merely rewrite its subject lines. Imagine the change had never landed and you are composing the commit history from scratch, choosing the decomposition a careful reviewer would be happiest to receive. The original title, body, and commit messages are context only — assume they are sloppy.

Decomposition principles (apply aggressively):
  - Separate mechanical / non-behavioral changes (renames, extractions, reorderings, deletions of now-unused code, whitespace, config bumps) from behavioral changes (bug fixes, new logic).
  - When the same commit both adds a new code path and deletes an old one, split them when the diff supports it: the add stands alone and makes sense as a precursor; the delete (and associated obsolete tests/fixtures) follows as a cleanup.
  - Test changes are not automatically their own commit. Co-locate test changes with the production change they cover. Only split tests out when they are genuinely independent (e.g., deleting obsolete fixtures, adding coverage for pre-existing behavior).
  - Fixture and cassette deletions that become obsolete because of a code change belong in a cleanup commit after the code change, not folded into it.
  - Prefer more, smaller, atomic commits over one big one. Each commit should be independently reviewable, independently revertable, and tell a single story. Aim for 3–6 commits for a non-trivial PR; fewer only if the change genuinely does not decompose.
  - Order commits so each one leaves the tree in a coherent state: refactors / preparations first, then the behavioral change, then cleanups.

HARD CONSTRAINT (enforced by a deterministic post-check): the union of all your commits' diffs MUST exactly match the PR's full diff, line for line, per file. You may split, regroup, and reorder changes across commits, but you may NOT add, remove, rename, or alter any line of code. Every '+' line and every '-' line in the PR's diff must appear exactly once across your commits, attributed to the same file path. No inventing code. No "cleaning up" code. No omissions.

If any change in the PR is impossible to attribute cleanly to a single commit, keep it in whichever commit it fits best — but do not drop it and do not duplicate it.`

const outputSpec = `Output format: mimic 'git log -p' exactly. Nothing before the first commit, nothing after the last commit. No preamble, no headings, no summary, no trailing notes. Just commits.

Each commit is structured as:

commit <40-char lowercase hex sha>          (fabricate a plausible sha; any 40-char hex is fine, keep them distinct)
Author: <Name> <<email>>                     (use the PR author if known; otherwise "PR Author <author@example.com>")
Date:   <Day Mon DD HH:MM:SS YYYY +0000>     (any plausible date; increment by minutes across commits)

    <subject line — imperative mood, ≤72 chars>

    <optional body paragraphs, wrapped at ~72 chars, each line indented by 4 spaces>

<unified diff for this commit, in standard 'git diff' format>

Rules for the diff portion of each commit:
  - Start each file with: diff --git a/<path> b/<path>
  - Include the standard headers (index line optional but encouraged; --- a/<path> and +++ b/<path> are REQUIRED for modified files).
  - Include @@ hunk headers. Line numbers inside @@ may be approximate — the post-check ignores them.
  - Every '+' and '-' line must be copied verbatim from the PR's diff (same content, same file). Context lines can be drawn from the PR's diff too.
  - Do not introduce any '+' or '-' line that is not in the PR's diff. Do not drop any.

Between commits, separate with a single blank line. Do not wrap commits in code fences. Do not use markdown anywhere. This is plain text that should look like the output of 'git log -p --reverse'.

Organize the commits to tell a coherent story of the change — logical, atomic, each independently reviewable — but the sum must still equal the original PR's diff.`

type prAuthor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
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
	model := "sonnet"
	var pr string
	demo := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			printUsage()
			return nil
		case a == "--model":
			if i+1 >= len(args) {
				return errors.New("--model requires a value")
			}
			model = args[i+1]
			i++
		case strings.HasPrefix(a, "--model="):
			model = strings.TrimPrefix(a, "--model=")
		case a == "--demo":
			demo = true
		case strings.HasPrefix(a, "-"):
			printUsage()
			return fmt.Errorf("unknown flag: %s", a)
		default:
			if pr != "" {
				printUsage()
				return errors.New("unexpected extra argument")
			}
			pr = a
		}
	}
	if demo {
		return runTUI(demoFixture)
	}
	if pr == "" {
		printUsage()
		return errors.New("missing PR URL")
	}
	if err := validatePRURL(pr); err != nil {
		return err
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("`gh` not found on PATH — install GitHub CLI and run `gh auth login`")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return errors.New("`claude` not found on PATH — install Claude Code")
	}

	t := startOpeningPR()
	meta, err := fetchMeta(pr)
	t.end()
	if err != nil {
		return fmt.Errorf("fetching PR metadata: %w", err)
	}

	t = startReadingDiff(meta.ChangedFiles, meta.Additions, meta.Deletions)
	diff, err := fetchDiff(pr)
	t.end()
	if err != nil {
		return fmt.Errorf("fetching PR diff: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return errors.New("PR diff is empty")
	}

	prompt := buildPrompt(meta, diff)

	t = startGhostWriting(model)
	resp, err := runClaude(prompt, model)
	t.end()
	if err != nil {
		return fmt.Errorf("running claude: %w", err)
	}

	t = startVerifying()
	violations := verifyDiffParity(diff, resp)
	t.end()
	if len(violations) > 0 {
		statusParityFail(countLineDivergences(diff, resp))
	} else {
		statusLGTM()
	}
	return runTUI(resp)
}

// status prints a progress line to stderr in the rose palette.
func status(msg string) {
	fmt.Fprintln(os.Stderr, styleHot.Render("rosy:")+" "+styleSubtle.Render(msg))
}

// statusWarn is like status but uses the crimson "del" tone, to visually flag
// notes the user should read (e.g. a parity-failure ribbon).
func statusWarn(msg string) {
	fmt.Fprintln(os.Stderr, styleDel.Render("rosy:")+" "+styleSubtle.Render(msg))
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// fileDiff holds the multisets of added/removed lines for a single file.
// Content only — line numbers and context lines are ignored, since the
// point is to confirm that no code was invented, dropped, or altered.
type fileDiff struct {
	added   []string
	removed []string
}

// parseDiffFiles walks a unified-diff stream and returns a map from file
// path to the added/removed line content for that file. It is tolerant of
// 'git log -p'-style output (commit headers, indented messages) interleaved
// with the diffs: anything that is not clearly inside a diff hunk is
// ignored.
func parseDiffFiles(text string) map[string]*fileDiff {
	files := map[string]*fileDiff{}
	var currentPath string
	inHunk := false

	ensure := func(path string) *fileDiff {
		fd, ok := files[path]
		if !ok {
			fd = &fileDiff{}
			files[path] = fd
		}
		return fd
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			inHunk = false
			currentPath = parseDiffGitPath(line)
			if currentPath != "" {
				ensure(currentPath)
			}
			continue
		}
		if strings.HasPrefix(line, "--- ") {
			inHunk = false
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			// '+++ b/<path>' is the authoritative destination path. Fall
			// back to it when 'diff --git' was missing or malformed.
			if currentPath == "" {
				p := strings.TrimPrefix(line, "+++ ")
				p = strings.TrimPrefix(p, "b/")
				if p != "/dev/null" {
					currentPath = p
					ensure(currentPath)
				}
			}
			inHunk = false
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}
		if !inHunk || currentPath == "" {
			continue
		}
		// Inside a hunk the only valid line prefixes are ' ', '+', '-', '\'.
		// Anything else means the hunk has ended (e.g., a 'commit ...' line
		// for the next entry in git log -p).
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			files[currentPath].added = append(files[currentPath].added, line[1:])
		case '-':
			files[currentPath].removed = append(files[currentPath].removed, line[1:])
		case ' ', '\\':
			// context or '\ No newline at end of file' — skip
		default:
			inHunk = false
		}
	}

	return files
}

func parseDiffGitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		return ""
	}
	a := strings.TrimPrefix(parts[0], "a/")
	b := strings.TrimPrefix(parts[1], "b/")
	if b != "" {
		return b
	}
	return a
}

// verifyDiffParity compares the PR's diff to the generated output and
// returns a list of violations when they do not represent the same set of
// changes. A successful return (empty slice) means every '+' and '-' line
// in the PR's diff appears exactly once in the generated commits, with the
// same file path, and no extra +/- lines were invented.
func verifyDiffParity(prDiff, generated string) []string {
	want := parseDiffFiles(prDiff)
	got := parseDiffFiles(generated)

	var violations []string

	// Files present in the PR but missing or wrong in the output.
	for path, w := range want {
		g, ok := got[path]
		if !ok {
			violations = append(violations, fmt.Sprintf("missing file in generated output: %s", path))
			continue
		}
		if d := describeMultisetDiff("added", path, w.added, g.added); d != "" {
			violations = append(violations, d)
		}
		if d := describeMultisetDiff("removed", path, w.removed, g.removed); d != "" {
			violations = append(violations, d)
		}
	}

	// Files the model invented that the PR does not touch.
	for path := range got {
		if _, ok := want[path]; !ok {
			violations = append(violations, fmt.Sprintf("generated output touches file not in PR: %s", path))
		}
	}

	sort.Strings(violations)
	return violations
}

// countLineDivergences counts the total number of +/- lines that don't
// reconcile between the PR's diff and the generated output, across every
// file. Used for a human-readable "N lines drifted" status message; it is
// not used to gate output.
func countLineDivergences(prDiff, generated string) int {
	want := parseDiffFiles(prDiff)
	got := parseDiffFiles(generated)
	total := 0
	for path, w := range want {
		g, ok := got[path]
		if !ok {
			total += len(w.added) + len(w.removed)
			continue
		}
		e1, m1 := multisetSymmetricDiff(w.added, g.added)
		e2, m2 := multisetSymmetricDiff(w.removed, g.removed)
		total += len(e1) + len(m1) + len(e2) + len(m2)
	}
	for path, g := range got {
		if _, ok := want[path]; !ok {
			total += len(g.added) + len(g.removed)
		}
	}
	return total
}

func describeMultisetDiff(kind, path string, want, got []string) string {
	if multisetEqual(want, got) {
		return ""
	}
	wantCount := len(want)
	gotCount := len(got)
	extra, missing := multisetSymmetricDiff(want, got)
	var sample string
	if len(missing) > 0 {
		sample = fmt.Sprintf(" missing: %q", firstLineSample(missing[0]))
	} else if len(extra) > 0 {
		sample = fmt.Sprintf(" extra: %q", firstLineSample(extra[0]))
	}
	return fmt.Sprintf("%s: %s lines differ (PR has %d, generated has %d;%s)",
		path, kind, wantCount, gotCount, sample)
}

func firstLineSample(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

func multisetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ca := countMap(a)
	cb := countMap(b)
	if len(ca) != len(cb) {
		return false
	}
	for k, v := range ca {
		if cb[k] != v {
			return false
		}
	}
	return true
}

// multisetSymmetricDiff returns (lines in got but not want, lines in want but not got).
func multisetSymmetricDiff(want, got []string) (extra, missing []string) {
	cw := countMap(want)
	cg := countMap(got)
	for k, v := range cg {
		diff := v - cw[k]
		for i := 0; i < diff; i++ {
			extra = append(extra, k)
		}
	}
	for k, v := range cw {
		diff := v - cg[k]
		for i := 0; i < diff; i++ {
			missing = append(missing, k)
		}
	}
	return extra, missing
}

func countMap(xs []string) map[string]int {
	m := make(map[string]int, len(xs))
	for _, x := range xs {
		m[x]++
	}
	return m
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
	b.WriteString("PR CONTEXT\n\n")

	fmt.Fprintf(&b, "Original title: %s\n", m.Title)
	author := m.Author.Login
	if author == "" {
		author = m.Author.Name
	}
	if author != "" {
		email := m.Author.Email
		if email == "" {
			email = fmt.Sprintf("%s@users.noreply.github.com", author)
		}
		fmt.Fprintf(&b, "Author: %s <%s>\n", author, email)
	}
	if m.BaseRefName != "" || m.HeadRefName != "" {
		fmt.Fprintf(&b, "Branch: %s -> %s\n", m.HeadRefName, m.BaseRefName)
	}
	if len(m.Labels) > 0 {
		names := make([]string, len(m.Labels))
		for i, l := range m.Labels {
			names[i] = l.Name
		}
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(&b, "Diffstat: %d file(s) changed, +%d / -%d\n\n",
		m.ChangedFiles, m.Additions, m.Deletions)

	b.WriteString("Original PR body:\n")
	if strings.TrimSpace(m.Body) == "" {
		b.WriteString("(empty)\n\n")
	} else {
		b.WriteString(m.Body)
		if !strings.HasSuffix(m.Body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Original commit count (FYI only — do not treat as a template; you are restructuring from scratch): ")
	fmt.Fprintf(&b, "%d\n\n", len(m.Commits))

	b.WriteString("PR DIFF (authoritative — your commits must collectively reproduce this exactly):\n\n")
	b.WriteString(diff)
	if !strings.HasSuffix(diff, "\n") {
		b.WriteString("\n")
	}

	return b.String()
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: rosy [--model <name>] <pr-url>")
	fmt.Fprintln(os.Stderr, "  <pr-url>        full GitHub PR URL, e.g. https://github.com/owner/repo/pull/123")
	fmt.Fprintln(os.Stderr, "  --model <name>  claude model to use (default: sonnet; e.g. sonnet, opus, haiku)")
}

func runClaude(prompt, model string) (string, error) {
	args := []string{"-p"}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.Command("claude", args...)
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
