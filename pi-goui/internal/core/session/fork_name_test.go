package session

import (
	"fmt"
	"os"
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

func TestCreateNumberedForkProjectScope(t *testing.T) {
	for _, outside := range []bool{false, true} {
		t.Run(fmt.Sprintf("outside=%t", outside), func(t *testing.T) {
			path, snapshot := forkSource(t)
			root := t.TempDir()
			const cwd = "/tmp" // Matches forkSource's project.
			if !outside {
				dir := filepath.Join(root, "sessions", "source")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				moved := filepath.Join(dir, "source.jsonl")
				if err := os.Rename(path, moved); err != nil {
					t.Fatal(err)
				}
				path = moved
			}
			writeSummaryFixture(t, root, summaryFixture(cwd, "[3]Original", "", "same project"))
			writeSummaryFixture(t, filepath.Join(root, "sessions", "other"), summaryFixture(t.TempDir(), "[99]Original", "", "other project"))
			foreignSibling := filepath.Join(filepath.Dir(path), "other-project.jsonl")
			if err := os.WriteFile(foreignSibling, summaryFixture(t.TempDir(), "[999]Original", "", "other project"), 0600); err != nil {
				t.Fatal(err)
			}
			want := "[4]Original"
			if outside {
				local := filepath.Join(filepath.Dir(path), "same-project.jsonl")
				if err := os.WriteFile(local, summaryFixture(cwd, "[5]Original", "", "same project"), 0600); err != nil {
					t.Fatal(err)
				}
				want = "[6]Original"
			}
			fork, err := CreateNumberedFork(root, path, "original", cwd, "Original", "selected", snapshot)
			if err != nil || fork.Name != want {
				t.Fatalf("project-scoped fork: %+v %v; want %q", fork, err, want)
			}
		})
	}
}

func TestCreateNumberedForkRequiresProject(t *testing.T) {
	path, snapshot := forkSource(t)
	if _, err := CreateNumberedFork(t.TempDir(), path, "original", "", "Original", "selected", snapshot); err == nil {
		t.Fatal("missing project silently fell back to a global scan")
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing project created a fork: %v %v", files, err)
	}
}

func TestCreateNumberedForkObservesRenames(t *testing.T) {
	path, snapshot := forkSource(t)
	root := t.TempDir()
	first, err := CreateNumberedFork(root, path, "original", "/tmp", "Original", "selected", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(first.Path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("{\"type\":\"session_info\",\"name\":\"[9]Original\"}\n")
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("rename: %v %v", err, closeErr)
	}
	next, err := CreateNumberedFork(root, path, "original", "/tmp", "Original", "selected", snapshot)
	if err != nil || next.Name != "[10]Original" {
		t.Fatalf("fork numbering ignored external rename: %+v %v", next, err)
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
