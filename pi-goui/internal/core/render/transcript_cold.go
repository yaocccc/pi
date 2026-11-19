package render

import (
	"runtime"
	"sync"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

type coldMarkdown struct {
	key  markdownKey
	rows []string
}

// Format independent Markdown bodies before assembling a large cold document.
// Only the pure user/assistant Markdown stage runs concurrently; tool providers,
// section caches, frames, metadata, hitboxes and final row order stay serial.
func prepareColdMarkdown(items []session.Item, width int, theme config.Theme) map[markdownKey]*coldMarkdown {
	lookup := make(map[markdownKey]*coldMarkdown)
	var work []*coldMarkdown
	for _, item := range items {
		w := width
		switch item.Role {
		case "user":
			w = userBubbleWidth(width)
		case "assistant":
		default:
			continue
		}
		key := markdownKey{item.Text, max(1, w-4)}
		// Don't multiply the peak memory of huge individual replies. They
		// still go through the normal serial path with no truncation.
		if key.text == "" || len(key.text) > 64<<10 || lookup[key] != nil {
			continue
		}
		d := &coldMarkdown{key: key}
		lookup[key] = d
		work = append(work, d)
	}
	workers := min(4, runtime.GOMAXPROCS(0), len(work))
	var group sync.WaitGroup
	for i := range workers {
		group.Go(func() {
			// Renderer instances own mutable parser state. Never share one
			// between workers or retain it across documents/themes.
			renderers := make(map[int]*markdownRenderer, 2)
			for j := i; j < len(work); j += workers {
				d := work[j]
				w := d.key.width
				r := renderers[w]
				if r == nil {
					r = &markdownRenderer{width: w, theme: theme}
					renderers[w] = r
				}
				d.rows = r.render(d.key.text)
			}
		})
	}
	group.Wait()
	return lookup
}
