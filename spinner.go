package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// spinnerFrames is the braille dot-cycle used for the status-line spinner.
var spinnerFrames = []string{"✦", "✧", "✶", "✸", "✹", "❋", "✿", "❀", "✾", "❁"}

// stepTimer animates a single status line with a spinner and a live elapsed
// counter. It prints to stderr and stays on one line (using \r and
// erase-to-end-of-line) until end() is called, at which point the spinner is
// replaced with a checkmark and the final elapsed time.
type stepTimer struct {
	mu    sync.Mutex
	msg   string
	start time.Time
	done  chan struct{}
	wg    sync.WaitGroup
	tty   bool
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

func (t *stepTimer) run() {
	defer t.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
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
	elapsed := formatElapsed(time.Since(t.start))
	line := styleHot.Render(mark) + " " +
		styleHot.Render("rosy:") + " " +
		styleSubtle.Render(t.currentMsg()) + " " +
		styleMuted.Render("..."+elapsed)
	fmt.Fprint(os.Stderr, "\r\x1b[K"+line)
}

func (t *stepTimer) end() {
	if !t.tty {
		return
	}
	close(t.done)
	t.wg.Wait()
	elapsed := formatElapsed(time.Since(t.start))
	line := styleHot.Render("❁") + " " +
		styleHot.Render("rosy:") + " " +
		styleSubtle.Render(t.currentMsg()) + " " +
		styleMuted.Render("..."+elapsed)
	fmt.Fprint(os.Stderr, "\r\x1b[K"+line+"\n")
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
