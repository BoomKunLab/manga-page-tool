package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const clearStatusLine = "\r\x1b[2K"

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type terminalProgress struct {
	file    *os.File
	mu      sync.Mutex
	message string
	frame   int
}

func withTerminalProgress(file *os.File) (io.Writer, func()) {
	if !term.IsTerminal(file.Fd()) || os.Getenv("TERM") == "dumb" {
		return file, func() {}
	}
	p := &terminalProgress{file: file}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				if p.message != "" {
					p.draw()
				}
				p.mu.Unlock()
			}
		}
	}()
	return p, func() {
		close(stop)
		<-done
		p.clear()
	}
}

func (p *terminalProgress) update(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idle := p.message == ""
	p.message = displayText(message)
	if idle {
		p.draw()
	}
}

func (p *terminalProgress) draw() {
	width, _, err := term.GetSize(p.file.Fd())
	if err != nil || width == 0 {
		width = 80
	}
	line := fmt.Sprintf("%c %s", spinnerFrames[p.frame], p.message)
	fmt.Fprint(p.file, clearStatusLine, ansi.Truncate(line, max(1, width-1), "…"))
	p.frame = (p.frame + 1) % len(spinnerFrames)
}

func (p *terminalProgress) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLine()
}

func (p *terminalProgress) clearLine() {
	if p.message != "" {
		fmt.Fprint(p.file, clearStatusLine)
		p.message = ""
	}
}

func (p *terminalProgress) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLine()
	return p.file.Write(data)
}

func reportProgress(out io.Writer, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if p, ok := out.(*terminalProgress); ok {
		p.update(message)
		return
	}
	fmt.Fprintln(out, message)
}

type pageProgress struct {
	out        io.Writer
	stage      string
	total      int
	step       int
	lastUpdate time.Time
}

func startPageProgress(out io.Writer, stage string, total int) pageProgress {
	reportProgress(out, "%s（対象: %d 枚）", stage, total)
	return pageProgress{out: out, stage: stage, total: total, step: max(1, (total+9)/10)}
}

// Report the first and last pages, milestones, and additional updates if
// individual pages are taking longer.
func (p *pageProgress) processing(index int, name string) {
	current := index + 1
	if _, ok := p.out.(*terminalProgress); ok {
		reportProgress(p.out, "[%d/%d] %s: %s", current, p.total, p.stage, name)
		return
	}
	if current != 1 && current != p.total && current%p.step != 0 && time.Since(p.lastUpdate) < 2*time.Second {
		return
	}
	fmt.Fprintf(p.out, "  [%d/%d] 処理中: %s\n", current, p.total, displayText(name))
	p.lastUpdate = time.Now()
}
