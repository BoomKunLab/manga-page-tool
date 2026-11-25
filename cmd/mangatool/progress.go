package main

import (
	"fmt"
	"io"
	"time"
)

type pageProgress struct {
	out        io.Writer
	total      int
	step       int
	lastUpdate time.Time
}

func startPageProgress(out io.Writer, stage string, total int) pageProgress {
	fmt.Fprintf(out, "%s（対象: %d 枚）\n", stage, total)
	return pageProgress{out: out, total: total, step: max(1, (total+9)/10)}
}

// Report the first and last pages, milestones, and additional updates if
// individual pages are taking longer.
func (p *pageProgress) processing(index int, name string) {
	current := index + 1
	if current != 1 && current != p.total && current%p.step != 0 && time.Since(p.lastUpdate) < 2*time.Second {
		return
	}
	fmt.Fprintf(p.out, "  [%d/%d] 処理中: %s\n", current, p.total, displayText(name))
	p.lastUpdate = time.Now()
}
