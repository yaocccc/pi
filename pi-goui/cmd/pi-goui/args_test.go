package main

import (
	"reflect"
	"testing"
)

func TestPiArgumentsCannotOverrideRPCOwnership(t *testing.T) {
	for _, args := range [][]string{{"--mode", "text"}, {"--session", "same.jsonl"}, {"--continue"}, {"--resume"}, {"--session-id", "same"}, {"--session-dir", "elsewhere"}, {"--no-session"}, {"--print"}, {"--", "--mode", "text"}, {"unexpected prompt"}, {"--export", "out.html"}, {"--model"}, {"--model="}} {
		if _, err := piArgs(args); err == nil {
			t.Fatalf("unsafe arguments accepted: %#v", args)
		}
	}
	got, err := piArgs([]string{"--model=provider/id", "--thinking", "high", "--models", "provider/a,provider/b", "--no-extensions", "-e", "local-extension.ts"})
	want := []string{"--model", "provider/id", "--thinking", "high", "--models", "provider/a,provider/b", "--no-extensions", "-e", "local-extension.ts"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("safe config flags: %#v, %v", got, err)
	}
}
