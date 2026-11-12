package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/session"
)

func TestSourceCheckArtifactCitationsAndUnknowns(t *testing.T) {
	item := session.Item{HasResult: true, Text: "Original guidance [p1]\n\nEND",
		Arguments: json.RawMessage(`{"claim":"Claim text"}`),
		Details:   json.RawMessage(`{"responseId":"research-1","sourceCount":1,"passageCount":1,"searchCount":2,"future":"TOP_EXTRA","artifact":{"id":"research-1","type":"research","query":"Claim text","provider":"exa","sources":[{"rank":1,"url":"https://example.test","title":"中文 source","quality":"official_docs","snippet":"snippet","custom":"SOURCE_EXTRA"}],"passages":[{"passage_id":"p1","source_url":"https://example.test","source_rank":1,"text":"first\n\n  quote END","origin":"provider_summary","extraction_span":{"start":0,"end":900719925474099312345},"custom":"PASSAGE_EXTRA"}],"claims":[{"claim":"Claim text","status":"unclear","supporting_passages":["p1"],"contradicting_passages":[],"rationale":"not enough evidence","confidence":0.42}],"future":"ARTIFACT_EXTRA"}}`)}
	got := strings.Join(webTestRows(t, (*renderer).sourceCheck, item, 60, false), "\n")
	for _, want := range []string{"Assessments", "status: unclear", "not enough evidence", "Cited passages", "p1", "origin: provider_summary", "first\n\n  quote END", "900719925474099312345", "SOURCE_EXTRA", "PASSAGE_EXTRA", "ARTIFACT_EXTRA", "TOP_EXTRA", item.Text} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	if strings.Count(got, "PASSAGE_EXTRA") != 1 || strings.Count(got, item.Text) != 1 {
		t.Fatal("repeated artifact dump")
	}
}

func TestSourceCheckMalformedArtifactRetained(t *testing.T) {
	item := session.Item{Details: json.RawMessage(`{"artifact":["unexpected",123],"sourceCount":{"future":"KEEP"}}`)}
	got := strings.Join(webTestRows(t, (*renderer).sourceCheck, item, 40, false), "\n")
	if !strings.Contains(got, "unexpected") || !strings.Contains(got, "123") || !strings.Contains(got, "KEEP") {
		t.Fatal(got)
	}
}
