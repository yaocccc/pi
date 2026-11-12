package tooldetail

import (
	"fmt"

	"pi-goui/internal/session"
)

// webSearch uses Details from buildSearchReturn, not structuredContent (which
// is not projected into session.Item). The provider's full text stays intact.
func (v *renderer) webSearch(item session.Item, args, details map[string]any) {
	v.webState(item, "Search")
	if _, ok := details["queries"]; ok {
		v.field(details, "queries", "Queries")
	} else if queries, ok := args["queries"]; ok {
		v.heading("Queries")
		v.value(queries, "")
	} else if query, ok := args["query"]; ok {
		v.heading("Query")
		v.value(query, "")
	}
	for _, f := range [][2]string{{"queryCount", "Queries"}, {"successfulQueries", "Succeeded"}, {"totalResults", "Results"}, {"searchId", "Search ID"}, {"fetchId", "Content ID"}} {
		v.webMeta(details, f[0], f[1])
	}
	v.field(details, "queryProviders", "Providers by query")
	v.webEntries(details, "curatedQueries", "Curated queries", "query", "")
	v.webTruncation(details)
	v.output(item.Text)
}

// Shared web presentation primitives deliberately leave unrecognized fields in
// the caller-owned map for Render's lossless Details fallback.
func (v *renderer) webState(item session.Item, label string) {
	color, state := v.theme.Muted, "waiting"
	if item.Running {
		color, state = v.theme.Warning, "running"
	} else if item.HasResult {
		state = "returned" // Partial failures do not imply successful results.
	}
	v.line(label+" · "+state, color)
}

func (v *renderer) webMeta(m map[string]any, key, label string) {
	value, ok := m[key]
	if !ok {
		return
	}
	switch value.(type) {
	case map[string]any, []any:
		v.field(m, key, label)
	default:
		v.line(label+": "+v.scalar(value), v.theme.Muted)
		delete(m, key)
	}
}

func (v *renderer) webTruncation(m map[string]any) {
	if truncated, ok := m["truncated"].(bool); ok {
		if truncated {
			v.line("Upstream output truncated · retrieve stored content for more", v.theme.Warning)
		} else {
			v.line("Upstream output complete", v.theme.Muted)
		}
		delete(m, "truncated")
	}
	for _, f := range [][2]string{{"originalChars", "Original characters"}, {"returnedChars", "Returned characters"}, {"omittedChars", "Omitted characters"}} {
		v.webMeta(m, f[0], f[1])
	}
}

// Only copies nested objects; decoding and displaying never modifies the
// session's input maps. Unfamiliar array members retain their original values.
func (v *renderer) webEntries(m map[string]any, key, label, titleKey, urlKey string) {
	entries, ok := m[key].([]any)
	if !ok {
		v.field(m, key, label)
		return
	}
	v.heading(label)
	if len(entries) == 0 {
		v.value(entries, "")
	}
	for i, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			v.value(raw, "")
			continue
		}
		entry = copyMap(entry)
		v.line(fmt.Sprintf("─ %d", i+1), v.theme.Muted)
		if title, ok := entry[titleKey].(string); ok {
			v.line(title, v.theme.Text)
			delete(entry, titleKey)
		}
		if url, ok := entry[urlKey].(string); ok && urlKey != "" {
			v.line(url, v.theme.Accent)
			delete(entry, urlKey)
		}
		if len(entry) > 0 {
			v.value(entry, "")
		}
	}
	delete(m, key)
}
