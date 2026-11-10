package session

import "testing"

func TestDisplayNamePrefersExplicitThenFirstUserInput(t *testing.T) {
	s := State{Items: []Item{{Role: "system", Text: "summary"}, {Role: "assistant", Text: "answer"}, {Role: "user", Text: "first input\ncontinued"}, {Role: "user", Text: "second input"}}}
	if got := DisplayName(s); got != "first input\ncontinued" {
		t.Fatal(got)
	}
	if s.Name != "" {
		t.Fatal("display fallback changed saved name")
	}
	s.Name = "Explicit name"
	if DisplayName(s) != s.Name {
		t.Fatal("explicit name must supersede preview")
	}
	if DisplayName(State{}) != "" {
		t.Fatal("empty session must remain blank")
	}
}
