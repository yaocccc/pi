package session

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestNextForkName(t *testing.T) {
	for _, tc := range []struct {
		source string
		names  []string
		want   string
	}{
		{"Original", nil, "[1]Original"},
		{"Original", []string{"[1]Original", "[3]Original", "[9]Other"}, "[4]Original"},
		{"[1]Original", []string{"[2]Original"}, "[3]Original"},
		{"[7]Original", nil, "[8]Original"},
		{"[notes]Original", nil, "[1][notes]Original"},
		{"中文名称", []string{"[1]中文名称"}, "[2]中文名称"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			var list []Summary
			for _, name := range tc.names {
				list = append(list, Summary{Name: name})
			}
			got, err := nextForkName(tc.source, list)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestCreateNumberedForkPersistsUniqueNames(t *testing.T) {
	path, snapshot := forkSource(t)
	root := t.TempDir() // Also exercises sessions opened outside the configured root.
	first, err := CreateNumberedFork(root, path, "original", "/tmp", "Original", "selected", snapshot)
	if err != nil || first.Name != "[1]Original" {
		t.Fatalf("first: %+v %v", first, err)
	}
	nested, err := CreateNumberedFork(root, first.Path, first.ID, first.Cwd, first.Name, "selected", first.Entries)
	if err != nil || nested.Name != "[2]Original" {
		t.Fatalf("nested: %+v %v", nested, err)
	}
	var wg sync.WaitGroup
	results := make(chan Fork, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fork, err := CreateNumberedFork(root, path, "original", "/tmp", "Original", "selected", snapshot)
			if err != nil {
				t.Error(err)
				return
			}
			results <- fork
		}()
	}
	wg.Wait()
	close(results)
	names := map[string]bool{}
	for fork := range results {
		names[fork.Name] = true
	}
	for i := 3; i <= 5; i++ {
		if !names[fmt.Sprintf("[%d]Original", i)] {
			t.Fatalf("unexpected names: %v", names)
		}
	}
	list, err := listSessions(filepath.Dir(path), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range list {
		delete(names, saved.Name)
	}
	if len(names) != 0 {
		t.Fatalf("names not persisted: %v", names)
	}
}
