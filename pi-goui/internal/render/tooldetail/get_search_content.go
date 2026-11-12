package tooldetail

import "pi-goui/internal/session"

// getSearchContent displays stored-result paging/find metadata. Offset zero,
// nextOffset null and unknown future fields must not disappear.
func (v *renderer) getSearchContent(item session.Item, args, details map[string]any) {
	v.webState(item, "Stored content")
	if _, ok := details["responseId"]; !ok {
		if id, exists := args["responseId"]; exists {
			v.line("Response ID: "+v.scalar(id), v.theme.Muted)
		}
	}
	for _, f := range [][2]string{{"responseId", "Response ID"}, {"type", "Kind"}, {"title", "Title"}, {"url", "Source"}, {"query", "Query"}, {"resultCount", "Results"}, {"contentLength", "Stored characters"}, {"offset", "Offset"}, {"limit", "Page limit"}, {"nextOffset", "Next offset"}, {"findMode", "Match mode"}} {
		v.webMeta(details, f[0], f[1])
	}
	if terms, ok := args["findText"]; ok {
		v.heading("Find passages")
		v.value(terms, "")
	}
	v.webTruncation(details)
	if item.Text != "" {
		v.heading("Excerpt")
		v.line(item.Text, v.theme.Text)
	}
}
