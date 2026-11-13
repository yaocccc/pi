package ui

import "fmt"

func (m *Model) scrollProgress(t *tab) string {
	total := t.doc.MessageCount
	if total <= 0 {
		return ""
	}
	end := t.offset + m.transcriptHeight(t)
	current := 0
	for _, block := range t.doc.Blocks {
		if block.Start < end {
			current = max(current, block.MessageIndex)
		}
	}
	if current == 0 && end > 0 {
		for _, block := range t.doc.Blocks {
			if block.MessageIndex > 0 {
				current = block.MessageIndex
				break
			}
		}
	}
	return fmt.Sprintf("%d/%d", min(current, total), total)
}
