package detail

import "pi-goui/internal/session"

// fetchContent preserves readable/raw/answer outputs exactly as received. A
// source may be stored remotely rather than included in this result.
func (v *Renderer) FetchContent(item session.Item, args, details map[string]any) {
	v.webState(item, "Fetch")
	if _, ok := details["urls"]; ok {
		v.Field(details, "urls", "Sources")
	} else if urls, ok := args["urls"]; ok {
		v.Heading("Sources")
		v.Value(urls, "")
	} else if url, ok := args["url"]; ok {
		v.Heading("Source")
		v.Value(url, "")
	}
	for _, f := range [][2]string{{"title", "Title"}, {"mode", "Mode"}, {"urlCount", "Sources"}, {"successful", "Succeeded"}, {"responseId", "Content ID"}, {"mimeType", "Media type"}, {"status", "HTTP status"}, {"totalChars", "Characters"}, {"totalBytes", "Bytes"}, {"shownBytes", "Shown bytes"}, {"totalLines", "Lines"}, {"shownLines", "Shown lines"}, {"imageCount", "Images"}} {
		v.webMeta(details, f[0], f[1])
	}
	v.webTruncation(details)
	if item.Text != "" {
		v.Heading("Content")
		v.Line(item.Text, v.Theme.Text)
	}
}
