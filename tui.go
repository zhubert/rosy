package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pane int

const (
	paneCommits pane = iota
	paneFiles
	paneDiff
)

var (
	colLight   = lipgloss.Color("#FFE4EC")
	colText    = lipgloss.Color("#E8C4CC")
	colMuted   = lipgloss.Color("#8B5A68")
	colSubtle  = lipgloss.Color("#C47B8A")
	colAccent  = lipgloss.Color("#FF80AB")
	colHot     = lipgloss.Color("#FF5C8A")
	colDeep    = lipgloss.Color("#C2185B")
	colBorder  = lipgloss.Color("#6B2E3F")
	colSelBG   = lipgloss.Color("#4A1E2A")
	colAdd     = lipgloss.Color("#F48FB1")
	colDel     = lipgloss.Color("#AD1457")
	colContext = lipgloss.Color("#9A7A85")

	styleAdd      = lipgloss.NewStyle().Foreground(colAdd)
	styleDel      = lipgloss.NewStyle().Foreground(colDel)
	styleContext  = lipgloss.NewStyle().Foreground(colContext)
	styleHunk     = lipgloss.NewStyle().Foreground(colDeep).Bold(true)
	styleFileHdr  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleDiffMeta = lipgloss.NewStyle().Foreground(colMuted)

	styleTitle    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleTitleDim = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	styleText     = lipgloss.NewStyle().Foreground(colText)
	styleMuted    = lipgloss.NewStyle().Foreground(colMuted)
	styleSubtle   = lipgloss.NewStyle().Foreground(colSubtle)
	styleSelected = lipgloss.NewStyle().Foreground(colLight).Background(colSelBG).Bold(true)
	styleHot      = lipgloss.NewStyle().Foreground(colHot).Bold(true)

	borderNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder)
	borderFocused = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colHot)
)

type model struct {
	commits   []parsedCommit
	commitIdx int
	fileIdx   int
	focus     pane

	width, height int

	commitTop int
	fileTop   int

	diff  viewport.Model
	ready bool

	ctx            *prContext
	confirmStep    int // 0=off 1="are you crazy?" 2=real confirm
	applyRequested bool
}

func newModel(commits []parsedCommit, ctx *prContext) model {
	return model{
		commits: commits,
		focus:   paneCommits,
		ctx:     ctx,
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m *model) currentFile() *parsedFile {
	if m.commitIdx >= len(m.commits) {
		return nil
	}
	files := m.commits[m.commitIdx].Files
	if m.fileIdx >= len(files) {
		return nil
	}
	return &files[m.fileIdx]
}

func (m *model) setDiffContent() {
	if m.commitIdx >= len(m.commits) {
		m.diff.SetContent(styleMuted.Render("(no commits)"))
		m.diff.GotoTop()
		return
	}
	w := m.diff.Width
	if w < 1 {
		w = 1
	}
	c := m.commits[m.commitIdx]
	var b strings.Builder

	// Commit preamble: subject (bold accent), blank, body (dimmed wrapped),
	// blank, soft divider, blank. The diff follows.
	b.WriteString(styleHot.Render(truncate(c.Subject, w)))
	b.WriteString("\n")
	if body := strings.TrimSpace(c.Body); body != "" {
		b.WriteString("\n")
		for _, para := range strings.Split(body, "\n") {
			if para == "" {
				b.WriteString("\n")
				continue
			}
			for _, ln := range wrapLine(para, w) {
				b.WriteString(styleSubtle.Render(ln))
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(strings.Repeat("─", w)))
	b.WriteString("\n\n")

	if f := m.currentFile(); f != nil {
		b.WriteString(colorizeDiff(f.Diff, w))
	} else {
		b.WriteString(styleMuted.Render("(no files)"))
	}

	m.diff.SetContent(b.String())
	m.diff.GotoTop()
}

func (m *model) onCommitChanged() {
	m.fileIdx = 0
	m.fileTop = 0
	m.setDiffContent()
}

func (m *model) onFileChanged() {
	m.setDiffContent()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		if !m.ready {
			m.ready = true
			m.setDiffContent()
		}
		return m, nil

	case tea.KeyMsg:
		if m.confirmStep > 0 {
			switch msg.String() {
			case "y":
				if m.confirmStep == 1 {
					m.confirmStep = 2
				} else {
					m.applyRequested = true
					return m, tea.Quit
				}
			case "n", "esc", "q", "ctrl+c":
				if msg.String() == "ctrl+c" {
					return m, tea.Quit
				}
				m.confirmStep = 0
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
			return m, nil
		case "i":
			if m.ctx != nil {
				m.confirmStep = 1
				return m, nil
			}
		}

		switch m.focus {
		case paneCommits:
			return m.updateCommits(msg)
		case paneFiles:
			return m.updateFiles(msg)
		case paneDiff:
			var cmd tea.Cmd
			m.diff, cmd = m.diff.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m model) updateCommits(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.commitIdx > 0 {
			m.commitIdx--
			m.onCommitChanged()
		}
	case "down", "j":
		if m.commitIdx < len(m.commits)-1 {
			m.commitIdx++
			m.onCommitChanged()
		}
	case "home", "g":
		m.commitIdx = 0
		m.onCommitChanged()
	case "end", "G":
		m.commitIdx = len(m.commits) - 1
		m.onCommitChanged()
	}
	return m, nil
}

func (m model) updateFiles(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	files := m.commits[m.commitIdx].Files
	switch msg.String() {
	case "up", "k":
		if m.fileIdx > 0 {
			m.fileIdx--
			m.onFileChanged()
		}
	case "down", "j":
		if m.fileIdx < len(files)-1 {
			m.fileIdx++
			m.onFileChanged()
		}
	case "home", "g":
		m.fileIdx = 0
		m.onFileChanged()
	case "end", "G":
		m.fileIdx = len(files) - 1
		m.onFileChanged()
	}
	return m, nil
}

// layout computes the inner content dimensions for each pane and configures
// the diff viewport. Called on every WindowSizeMsg.
func (m *model) layout() {
	if m.width < 40 || m.height < 10 {
		return
	}
	statusH := 1
	bodyH := m.height - statusH
	leftW := m.width / 3
	if leftW < 24 {
		leftW = 24
	}
	rightW := m.width - leftW
	commitsH := bodyH / 2
	filesH := bodyH - commitsH

	// Inside a rounded-border box: content area = outer - 2 cols, - 2 rows.
	// One row inside the diff pane is reserved for its header.
	diffW := rightW - 2
	diffH := bodyH - 2 - 1
	if diffW < 1 {
		diffW = 1
	}
	if diffH < 1 {
		diffH = 1
	}
	preservedY := 0
	if m.ready {
		preservedY = m.diff.YOffset
	}
	m.diff = viewport.New(diffW, diffH)
	m.diff.Style = lipgloss.NewStyle().Foreground(colText)
	if m.ready {
		m.diff.YOffset = preservedY
	}

	_ = leftW
	_ = commitsH
	_ = filesH
}

func (m model) View() string {
	if m.confirmStep > 0 {
		return m.renderConfirm()
	}
	if !m.ready || m.width < 40 || m.height < 10 {
		return styleMuted.Render("  rosy — terminal too small")
	}

	statusH := 1
	bodyH := m.height - statusH
	leftW := m.width / 3
	if leftW < 24 {
		leftW = 24
	}
	rightW := m.width - leftW
	commitsH := bodyH / 2
	filesH := bodyH - commitsH

	commitsBox := m.renderCommits(leftW, commitsH)
	filesBox := m.renderFiles(leftW, filesH)
	diffBox := m.renderDiff(rightW, bodyH)

	left := lipgloss.JoinVertical(lipgloss.Left, commitsBox, filesBox)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, diffBox)
	status := m.renderStatus()

	return lipgloss.JoinVertical(lipgloss.Left, body, status)
}

func (m model) renderCommits(outerW, outerH int) string {
	innerW := outerW - 2
	innerH := outerH - 2
	if innerH < 1 {
		innerH = 1
	}
	title := styleTitle.Render(" Commits ")
	if m.focus != paneCommits {
		title = styleTitleDim.Render(" Commits ")
	}

	visible := innerH - 1 // one line for the title
	if visible < 1 {
		visible = 1
	}

	top := m.commitTop
	if m.commitIdx < top {
		top = m.commitIdx
	}
	if m.commitIdx >= top+visible {
		top = m.commitIdx - visible + 1
	}
	if top < 0 {
		top = 0
	}

	var lines []string
	lines = append(lines, title)
	for i := 0; i < visible; i++ {
		idx := top + i
		if idx >= len(m.commits) {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, m.renderCommitRow(idx, innerW))
	}
	body := strings.Join(lines, "\n")
	border := borderNormal
	if m.focus == paneCommits {
		border = borderFocused
	}
	return border.Width(innerW).Height(innerH).Render(body)
}

func (m model) renderCommitRow(idx, innerW int) string {
	c := m.commits[idx]
	sha := c.ShortSHA()
	selected := idx == m.commitIdx

	cursor := "  "
	if selected {
		cursor = "▸ "
	}
	// Plain layout first so truncation is predictable, then style.
	maxSubj := innerW - len(cursor) - len(sha) - 1
	if maxSubj < 1 {
		maxSubj = 1
	}
	subj := truncate(c.Subject, maxSubj)

	if selected {
		line := cursor + sha + " " + subj
		pad := innerW - lipgloss.Width(line)
		if pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		return styleSelected.Render(line)
	}
	return cursor + styleSubtle.Render(sha) + " " + styleText.Render(subj)
}

func (m model) renderFiles(outerW, outerH int) string {
	innerW := outerW - 2
	innerH := outerH - 2
	if innerH < 1 {
		innerH = 1
	}
	title := styleTitle.Render(" Files ")
	if m.focus != paneFiles {
		title = styleTitleDim.Render(" Files ")
	}

	files := []parsedFile{}
	if m.commitIdx < len(m.commits) {
		files = m.commits[m.commitIdx].Files
	}

	visible := innerH - 1
	if visible < 1 {
		visible = 1
	}
	top := m.fileTop
	if m.fileIdx < top {
		top = m.fileIdx
	}
	if m.fileIdx >= top+visible {
		top = m.fileIdx - visible + 1
	}
	if top < 0 {
		top = 0
	}

	var lines []string
	lines = append(lines, title)
	for i := 0; i < visible; i++ {
		idx := top + i
		if idx >= len(files) {
			lines = append(lines, "")
			continue
		}
		f := files[idx]
		cursor := "  "
		selected := idx == m.fileIdx
		if selected {
			cursor = "▸ "
		}
		stats := fmt.Sprintf("+%d -%d", f.Adds, f.Dels)
		statsW := lipgloss.Width(stats) + 1
		pathW := innerW - lipgloss.Width(cursor) - statsW
		if pathW < 1 {
			pathW = 1
		}
		path := truncatePath(f.Path, pathW)
		pad := pathW - lipgloss.Width(path)
		if pad < 0 {
			pad = 0
		}

		if selected {
			line := cursor + path + strings.Repeat(" ", pad) + " " + stats
			extra := innerW - lipgloss.Width(line)
			if extra > 0 {
				line += strings.Repeat(" ", extra)
			}
			lines = append(lines, styleSelected.Render(line))
		} else {
			line := cursor + styleText.Render(path) + strings.Repeat(" ", pad) + " " + styleSubtle.Render(stats)
			lines = append(lines, line)
		}
	}

	body := strings.Join(lines, "\n")
	border := borderNormal
	if m.focus == paneFiles {
		border = borderFocused
	}
	return border.Width(innerW).Height(innerH).Render(body)
}

func (m model) renderDiff(outerW, outerH int) string {
	innerW := outerW - 2
	innerH := outerH - 2
	if innerH < 1 {
		innerH = 1
	}

	titleStyle := styleTitle
	pathStyle := styleText
	if m.focus != paneDiff {
		titleStyle = styleTitleDim
		pathStyle = styleMuted
	}

	var header string
	if f := m.currentFile(); f != nil {
		label := titleStyle.Render(" Diff ")
		remaining := innerW - lipgloss.Width(label) - 2
		if remaining < 1 {
			remaining = 1
		}
		header = label + "  " + pathStyle.Render(truncatePath(f.Path, remaining))
	} else {
		header = titleStyle.Render(" Diff ")
	}

	body := header + "\n" + m.diff.View()
	border := borderNormal
	if m.focus == paneDiff {
		border = borderFocused
	}
	return border.Width(innerW).Height(innerH).Render(body)
}

func (m model) renderStatus() string {
	focusName := map[pane]string{
		paneCommits: "commits",
		paneFiles:   "files",
		paneDiff:    "diff",
	}[m.focus]

	sep := styleMuted.Render(" · ")
	parts := []string{
		styleHot.Render("rosy"),
		sep,
		styleMuted.Render("focus ") + styleSubtle.Render(focusName),
		sep,
		styleMuted.Render("tab ") + styleSubtle.Render("cycle"),
		sep,
		styleMuted.Render("↑↓/jk ") + styleSubtle.Render("select"),
		sep,
		styleMuted.Render("q ") + styleSubtle.Render("quit"),
	}
	if m.ctx != nil {
		parts = append(parts, sep, styleMuted.Render("i ")+styleSubtle.Render("implement"))
	}
	return strings.Join(parts, "")
}

func (m model) renderConfirm() string {
	w := 60
	if m.width > 0 && m.width-8 < w {
		w = m.width - 8
	}
	if w < 24 {
		w = 24
	}

	var b strings.Builder
	line := func(s string) { b.WriteString(s); b.WriteString("\n") }

	if m.confirmStep == 1 {
		line(styleHot.Render("are you crazy?"))
		line("")
		line(styleSubtle.Render("rosy is about to rewrite your local branch history."))
		line("")
		line(styleMuted.Render("y") + styleSubtle.Render("  yes, in this case I am") + "   " + styleMuted.Render("n") + styleSubtle.Render("  cancel"))
	} else {
		ctx := m.ctx
		baseSHA := shortSHA(ctx.BaseSHA)
		headSHA := shortSHA(ctx.HeadSHA)

		line(styleHot.Render("apply rosy to " + ctx.Branch + "?"))
		line("")
		line(styleMuted.Render("rosy will:"))
		line("")
		line(styleMuted.Render("  1  ") + styleText.Render("gh pr checkout"))
		line(styleMuted.Render("     set local branch to PR head (" + headSHA + ")"))
		line("")
		line(styleMuted.Render("  2  ") + styleText.Render("git reset --hard " + baseSHA))
		line(styleMuted.Render("     rewind to PR base commit"))
		line("")
		line(styleMuted.Render("  3  ") + styleText.Render(fmt.Sprintf("apply %d commit%s", len(m.commits), pluralS(len(m.commits)))))
		line(styleMuted.Render("     rosy's reorganized history"))
		line("")
		line(styleMuted.Render("  4  ") + styleText.Render("verify final tree == "+headSHA))
		line(styleMuted.Render("     rolled back automatically on mismatch"))
		line("")
		line(styleHot.Render("rosy will NEVER force push."))
		line("")

		if len(ctx.Violations) == 0 {
			line(styleAdd.Render("parity ✓") + styleMuted.Render("  generated diff matches PR exactly"))
		} else {
			line(styleDel.Render(fmt.Sprintf("parity ✗  %d line%s drifted — apply with caution",
				ctx.DivergenceCount, pluralS(ctx.DivergenceCount))))
			line("")
			shown := ctx.Violations
			extra := 0
			if len(shown) > 4 {
				extra = len(shown) - 4
				shown = shown[:4]
			}
			for _, v := range shown {
				line(styleDel.Render("  · " + truncate(v, w-4)))
			}
			if extra > 0 {
				line(styleMuted.Render(fmt.Sprintf("  · +%d more", extra)))
			}
			line("")
		}

		line(styleMuted.Render("y") + styleSubtle.Render("  apply") + "   " + styleMuted.Render("n") + styleSubtle.Render("  cancel"))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colHot).
		Padding(1, 2).
		Width(w).
		Render(strings.TrimRight(b.String(), "\n"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func colorizeDiff(text string, width int) string {
	if width < 1 {
		width = 1
	}
	var style lipgloss.Style
	lines := strings.Split(text, "\n")
	var out []string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			style = styleFileHdr
		case strings.HasPrefix(l, "index "),
			strings.HasPrefix(l, "--- "),
			strings.HasPrefix(l, "+++ "),
			strings.HasPrefix(l, "new file"),
			strings.HasPrefix(l, "deleted file"),
			strings.HasPrefix(l, "similarity "),
			strings.HasPrefix(l, "rename "),
			strings.HasPrefix(l, "copy "):
			style = styleDiffMeta
		case strings.HasPrefix(l, "@@"):
			style = styleHunk
		case strings.HasPrefix(l, "+"):
			style = styleAdd
		case strings.HasPrefix(l, "-"):
			style = styleDel
		default:
			style = styleContext
		}
		for _, chunk := range wrapLine(l, width) {
			out = append(out, style.Render(chunk))
		}
	}
	return strings.Join(out, "\n")
}

// wrapLine splits a line into rune-safe chunks of at most `width` display
// columns, preserving the original content so nothing is silently truncated.
// Empty input returns a single empty chunk so blank lines are preserved.
func wrapLine(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	if s == "" {
		return []string{""}
	}
	runes := []rune(s)
	var chunks []string
	start := 0
	w := 0
	for i, r := range runes {
		rw := runewidth(r)
		if w+rw > width && i > start {
			chunks = append(chunks, string(runes[start:i]))
			start = i
			w = 0
		}
		w += rw
	}
	chunks = append(chunks, string(runes[start:]))
	return chunks
}

// runewidth is a minimal display-width approximation: 0 for control chars,
// 2 for wide east-asian/emoji, 1 otherwise. lipgloss.Width on single runes
// would work too but this avoids the per-rune allocation.
func runewidth(r rune) int {
	if r < 0x20 || r == 0x7f {
		return 0
	}
	if r >= 0x1100 &&
		(r <= 0x115f ||
			r == 0x2329 || r == 0x232a ||
			(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
			(r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe30 && r <= 0xfe4f) ||
			(r >= 0xff00 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6) ||
			(r >= 0x1f300 && r <= 0x1faff) ||
			(r >= 0x20000 && r <= 0x2fffd) ||
			(r >= 0x30000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// simple rune-safe truncate; assumes no ANSI in s
	r := []rune(s)
	if w <= 1 {
		return string(r[:1])
	}
	return string(r[:w-1]) + "…"
}

// truncatePath keeps the tail of a path when it's too long so the filename
// stays visible, e.g. ".../pkg/foo/bar.go".
func truncatePath(p string, w int) string {
	if lipgloss.Width(p) <= w {
		return p
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(p)
	return "…" + string(r[len(r)-(w-1):])
}

func runTUI(generated string, ctx *prContext) (bool, error) {
	commits := parseCommits(generated)
	if len(commits) == 0 {
		return false, fmt.Errorf("no commits parsed from generated output")
	}
	p := tea.NewProgram(newModel(commits, ctx), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return false, err
	}
	if m, ok := final.(model); ok {
		return m.applyRequested, nil
	}
	return false, nil
}
