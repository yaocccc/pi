package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// userContent projects one native user record into one message. Inline references
// already present in RPC text refer to the ordered image blocks that follow it.
func userContent(raw json.RawMessage) (string, []Image) {
	content := blocks(raw)
	var textParts []string
	for _, c := range content {
		if c.Type == "text" {
			textParts = append(textParts, c.Text)
		}
	}
	allText := strings.Join(textParts, "\n")
	var out strings.Builder
	var images []Image
	lastImage := false
	space := func() {
		if out.Len() > 0 {
			r, _ := utf8.DecodeLastRuneInString(out.String())
			if !unicode.IsSpace(r) {
				out.WriteByte(' ')
			}
		}
	}
	for _, c := range content {
		switch c.Type {
		case "text":
			if c.Text == "" {
				continue
			}
			if out.Len() > 0 {
				if lastImage {
					r, _ := utf8.DecodeRuneInString(c.Text)
					if !unicode.IsSpace(r) {
						space()
					}
				} else {
					out.WriteByte('\n')
				}
			}
			out.WriteString(c.Text)
			lastImage = false
		case "image":
			images = append(images, Image{Data: c.Data, MimeType: c.MimeType})
			marker := fmt.Sprintf("[image%d]", len(images))
			if strings.Contains(allText, marker) {
				continue
			}
			space()
			out.WriteString(marker)
			lastImage = true
		}
	}
	return out.String(), images
}

func userText(raw json.RawMessage) string {
	text, _ := userContent(raw)
	return text
}
