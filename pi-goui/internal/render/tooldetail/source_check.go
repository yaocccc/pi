package tooldetail

import "pi-goui/internal/session"

// sourceCheck follows the ResearchArtifact embedded in Details by the web
// extension. It never invents a verdict from source counts or search success.
func (v *renderer) sourceCheck(item session.Item, args, details map[string]any) {
	v.webState(item, "Source check")
	if claim, ok := args["claim"]; ok {
		v.heading("Claim")
		v.value(claim, "")
	}
	for _, f := range [][2]string{{"responseId", "Research ID"}, {"sourceCount", "Sources"}, {"passageCount", "Passages"}, {"searchCount", "Searches"}} {
		v.webMeta(details, f[0], f[1])
	}
	if artifact, ok := details["artifact"].(map[string]any); ok {
		artifact = copyMap(artifact)
		for _, f := range [][2]string{{"id", "Artifact ID"}, {"query", "Research query"}, {"provider", "Provider"}} {
			v.webMeta(artifact, f[0], f[1])
		}
		v.webEntries(artifact, "claims", "Assessments", "claim", "")
		v.field(artifact, "summary", "Summary")
		v.webEntries(artifact, "sources", "Sources", "title", "url")
		v.webEntries(artifact, "passages", "Cited passages", "passage_id", "source_url")
		v.field(artifact, "errors", "Research errors")
		if len(artifact) > 0 {
			v.heading("Artifact metadata")
			v.value(artifact, "")
		}
		delete(details, "artifact")
	}
	// The prose can contain guidance and citations absent from the artifact.
	// Do not drop it merely because an artifact was rendered.
	v.output(item.Text)
}
