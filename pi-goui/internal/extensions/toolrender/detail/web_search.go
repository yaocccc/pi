package detail

import (
	"fmt"

	"pi-goui/internal/core/session"
)

// webSearch uses Details from buildSearchReturn, not structuredContent (which
// is not projected into session.Item). The provider's full text stays intact.
func (v *Renderer) WebSearch(item session.Item, args, details map[string]any) {
	v.webState(item, "Search")
	if _, ok := details["queries"]; ok {
		v.Field(details, "queries", "Queries")
	} else if queries, ok := args["queries"]; ok {
		v.Heading("Queries")
		v.Value(queries, "")
	} else if query, ok := args["query"]; ok {
		v.Heading("Query")
		v.Value(query, "")
	}
	for _, f := range [][2]string{{"queryCount", "Queries"}, {"successfulQueries", "Succeeded"}, {"totalResults", "Results"}, {"searchId", "Search ID"}, {"fetchId", "Content ID"}} {
		v.webMeta(details, f[0], f[1])
	}
	v.Field(details, "queryProviders", "Providers by query")
	v.WebEntries(details, "curatedQueries", "Curated queries", "query", "")
	v.webTruncation(details)
	v.Output(item.Text)
}

// Shared web presentation primitives deliberately leave unrecognized fields in
// the caller-owned map for Render's lossless Details fallback.
func (v *Renderer) webState(item session.Item, label string) {
	color, state := v.Theme.Muted, "waiting"
	if item.Running {
		color, state = v.Theme.Warning, "running"
	} else if item.HasResult {
		state = "returned" // Partial failures do not imply successful results.
	}
	v.Line(label+" · "+state, color)
}

func (v *Renderer) webMeta(m map[string]any, key, label string) {
	value, ok := m[key]
	if !ok {
		return
	}
	switch value.(type) {
	case map[string]any, []any:
		v.Field(m, key, label)
	default:
		v.Line(label+": "+v.Scalar(value), v.Theme.Muted)
		delete(m, key)
	}
}

func (v *Renderer) webTruncation(m map[string]any) {
	if truncated, ok := m["truncated"].(bool); ok {
		if truncated {
			v.Line("Upstream output truncated · retrieve stored content for more", v.Theme.Warning)
		} else {
			v.Line("Upstream output complete", v.Theme.Muted)
		}
		delete(m, "truncated")
	}
	for _, f := range [][2]string{{"originalChars", "Original characters"}, {"returnedChars", "Returned characters"}, {"omittedChars", "Omitted characters"}} {
		v.webMeta(m, f[0], f[1])
	}
}

// Only copies nested objects; decoding and displaying never modifies the
// session's input maps. Unfamiliar array members retain their original values.
func (v *Renderer) WebEntries(m map[string]any, key, label, titleKey, urlKey string) {
	entries, ok := m[key].([]any)
	if !ok {
		v.Field(m, key, label)
		return
	}
	v.Heading(label)
	if len(entries) == 0 {
		v.Value(entries, "")
	}
	for i, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			v.Value(raw, "")
			continue
		}
		entry = copyMap(entry)
		v.Line(fmt.Sprintf("─ %d", i+1), v.Theme.Muted)
		if title, ok := entry[titleKey].(string); ok {
			v.Line(title, v.Theme.Text)
			delete(entry, titleKey)
		}
		if url, ok := entry[urlKey].(string); ok && urlKey != "" {
			v.Line(url, v.Theme.Accent)
			delete(entry, urlKey)
		}
		if len(entry) > 0 {
			v.Value(entry, "")
		}
	}
	delete(m, key)
}
