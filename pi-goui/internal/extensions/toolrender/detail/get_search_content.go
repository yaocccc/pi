package detail

import "pi-goui/internal/core/session"

// getSearchContent displays stored-result paging/find metadata. Offset zero,
// nextOffset null and unknown future fields must not disappear.
func (v *Renderer) GetSearchContent(item session.Item, args, details map[string]any) {
	v.webState(item, "Stored content")
	if _, ok := details["responseId"]; !ok {
		if id, exists := args["responseId"]; exists {
			v.Line("Response ID: "+v.Scalar(id), v.Theme.Muted)
		}
	}
	for _, f := range [][2]string{{"responseId", "Response ID"}, {"type", "Kind"}, {"title", "Title"}, {"url", "Source"}, {"query", "Query"}, {"resultCount", "Results"}, {"contentLength", "Stored characters"}, {"offset", "Offset"}, {"limit", "Page limit"}, {"nextOffset", "Next offset"}, {"findMode", "Match mode"}} {
		v.webMeta(details, f[0], f[1])
	}
	if terms, ok := args["findText"]; ok {
		v.Heading("Find passages")
		v.Value(terms, "")
	}
	v.webTruncation(details)
	if item.Text != "" {
		v.Heading("Excerpt")
		v.Line(item.Text, v.Theme.Text)
	}
}
