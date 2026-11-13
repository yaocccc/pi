package detail

import "pi-goui/internal/session"

// sourceCheck follows the ResearchArtifact embedded in Details by the web
// extension. It never invents a verdict from source counts or search success.
func (v *Renderer) SourceCheck(item session.Item, args, details map[string]any) {
	v.webState(item, "Source check")
	if claim, ok := args["claim"]; ok {
		v.Heading("Claim")
		v.Value(claim, "")
	}
	for _, f := range [][2]string{{"responseId", "Research ID"}, {"sourceCount", "Sources"}, {"passageCount", "Passages"}, {"searchCount", "Searches"}} {
		v.webMeta(details, f[0], f[1])
	}
	if artifact, ok := details["artifact"].(map[string]any); ok {
		artifact = copyMap(artifact)
		for _, f := range [][2]string{{"id", "Artifact ID"}, {"query", "Research query"}, {"provider", "Provider"}} {
			v.webMeta(artifact, f[0], f[1])
		}
		v.WebEntries(artifact, "claims", "Assessments", "claim", "")
		v.Field(artifact, "summary", "Summary")
		v.WebEntries(artifact, "sources", "Sources", "title", "url")
		v.WebEntries(artifact, "passages", "Cited passages", "passage_id", "source_url")
		v.Field(artifact, "errors", "Research errors")
		if len(artifact) > 0 {
			v.Heading("Artifact metadata")
			v.Value(artifact, "")
		}
		delete(details, "artifact")
	}
	// The prose can contain guidance and citations absent from the artifact.
	// Do not drop it merely because an artifact was rendered.
	v.Output(item.Text)
}
