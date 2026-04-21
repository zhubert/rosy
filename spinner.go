package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
)

// spinnerFrames is the braille dot-cycle used for the status-line spinner.
var spinnerFrames = []string{"✦", "✧", "✶", "✸", "✹", "❋", "✿", "❀", "✾", "❁"}

// stepTimer animates a single status line with a spinner and a live elapsed
// counter. It prints to stderr and stays on one line (using \r and
// erase-to-end-of-line) until end() is called, at which point the spinner is
// replaced with a checkmark and the final elapsed time.
type stepTimer struct {
	mu        sync.Mutex
	msg       string
	allLines  []string // all streamed lines, newest last
	prevCount int      // how many preview lines were printed in the last render
	start     time.Time
	done      chan struct{}
	wg        sync.WaitGroup
	tty       bool
}

func startStep(msg string) *stepTimer {
	t := &stepTimer{
		msg:   msg,
		start: time.Now(),
		done:  make(chan struct{}),
		tty:   isStderrTTY(),
	}
	if t.tty {
		t.render(spinnerFrames[0])
		t.wg.Add(1)
		go t.run()
	} else {
		fmt.Fprintln(os.Stderr, styleHot.Render("rosy:")+" "+styleSubtle.Render(msg))
	}
	return t
}

func (t *stepTimer) setMsg(msg string) {
	t.mu.Lock()
	t.msg = msg
	t.mu.Unlock()
}

func (t *stepTimer) currentMsg() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.msg
}

// addLine appends a line to the preview buffer.
func (t *stepTimer) addLine(line string) {
	t.mu.Lock()
	t.allLines = append(t.allLines, line)
	t.mu.Unlock()
}

// previewLines returns the tail of allLines that fits in the available vertical
// space: terminal height minus 1 (spinner row) minus 1 (bottom margin).
func (t *stepTimer) previewLines() []string {
	_, h, err := term.GetSize(os.Stderr.Fd())
	cap := 5 // safe fallback
	if err == nil && h > 3 {
		cap = h - 2 // 1 for spinner, 1 bottom margin
	}
	if len(t.allLines) <= cap {
		return t.allLines
	}
	return t.allLines[len(t.allLines)-cap:]
}

func (t *stepTimer) run() {
	defer t.wg.Done()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			frame = (frame + 1) % len(spinnerFrames)
			t.render(spinnerFrames[frame])
		}
	}
}

func (t *stepTimer) render(mark string) {
	t.mu.Lock()
	msg := t.msg
	lines := t.previewLines()
	prev := t.prevCount
	t.mu.Unlock()

	// Move cursor up to overwrite the spinner line and any preview lines
	// printed during the last render.
	if prev > 0 {
		fmt.Fprintf(os.Stderr, "\x1b[%dA", prev)
	}

	elapsed := formatElapsed(time.Since(t.start))
	spinnerLine := styleHot.Render(mark) + " " +
		styleHot.Render("rosy:") + " " +
		styleSubtle.Render(msg) + " " +
		styleMuted.Render("..."+elapsed)
	fmt.Fprint(os.Stderr, "\r\x1b[K"+spinnerLine)

	for _, l := range lines {
		fmt.Fprint(os.Stderr, "\n\r\x1b[K"+styleMuted.Render("  "+truncateLine(l, 72)))
	}
	// Clear any lines left over from a previous render that had more entries.
	for i := len(lines); i < prev; i++ {
		fmt.Fprint(os.Stderr, "\n\r\x1b[K")
	}

	newCount := len(lines)
	if prev > newCount {
		newCount = prev
	}

	t.mu.Lock()
	t.prevCount = newCount
	t.mu.Unlock()
}

func truncateLine(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func (t *stepTimer) end() {
	if !t.tty {
		return
	}
	close(t.done)
	t.wg.Wait()

	t.mu.Lock()
	prev := t.prevCount
	msg := t.msg
	t.mu.Unlock()

	if prev > 0 {
		fmt.Fprintf(os.Stderr, "\x1b[%dA", prev)
	}

	elapsed := formatElapsed(time.Since(t.start))
	line := styleHot.Render("❁") + " " +
		styleHot.Render("rosy:") + " " +
		styleSubtle.Render(msg) + " " +
		styleMuted.Render("..."+elapsed)
	// \x1b[0J erases from the cursor to end of screen, clearing the preview lines.
	fmt.Fprint(os.Stderr, "\r\x1b[K"+line+"\x1b[0J\n")
}

func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

func isStderrTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
