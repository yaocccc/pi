package updates

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeManager = `
import {readFileSync,existsSync} from 'node:fs';
import {join} from 'node:path';
export class DefaultPackageManager {
 constructor(o){this.o=o;if(Object.keys(o.settingsManager.getProjectSettings()).length || o.settingsManager.isProjectTrusted())throw Error('project settings exposed');o.settingsManager.getNpmCommand();}
 parseSource(source){if(source.startsWith('npm:'))return {type:'npm',pinned:source.slice(4).includes('@')};if(source.startsWith('git:'))return {type:'git',pinned:source.includes('@')};return {type:'local'};}
 getPackageIdentity(source){return source.split('@')[0];}
 getNpmInstallPath(){return join(this.o.agentDir,'installed');}
 getGitInstallPath(){return join(this.o.agentDir,'git-package');}
 async checkForAvailableUpdates(){return JSON.parse(readFileSync(join(this.o.agentDir,'available.json'),'utf8'));}
}
`
const fakeCLI = `#!/usr/bin/env node
import {appendFileSync,existsSync} from 'node:fs';
import {join} from 'node:path';
appendFileSync(join(process.env.PI_CODING_AGENT_DIR,'calls.jsonl'),JSON.stringify(process.argv.slice(2))+'\n');
if(existsSync(join(process.env.PI_CODING_AGENT_DIR,'fail-update'))){console.error('SECRET registry password');process.exit(1);}
`

type fixture struct {
	cfg      Config
	root     string
	settings []byte
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T) fixture {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required for bridge fixture")
	}
	t.Setenv("PI_OFFLINE", "")
	base := t.TempDir()
	root := filepath.Join(base, "pi")
	agent := filepath.Join(base, "agent")
	put(t, filepath.Join(root, "package.json"), `{"name":"@earendil-works/pi-coding-agent","version":"1.2.3","type":"module","bin":{"pi":"dist/cli.js"}}`)
	put(t, filepath.Join(root, "dist/cli.js"), fakeCLI)
	put(t, filepath.Join(root, "dist/core/package-manager.js"), fakeManager)
	put(t, filepath.Join(root, "dist/utils/version-check.js"), `export async function checkForNewPiVersion(current){if(current!=='1.2.3')throw 0;return {version:'1.2.4'};}`)
	settings := `{"packages":["npm:example","npm:pinned@1.0.0","./local"]}`
	put(t, filepath.Join(agent, "settings.json"), settings)
	put(t, filepath.Join(agent, "available.json"), `[{"source":"npm:example","displayName":"example","type":"npm","scope":"user"},{"source":"npm:pinned@1.0.0","displayName":"pinned","type":"npm","scope":"user"},{"source":"./local","type":"local","scope":"user"},{"source":"npm:project","type":"npm","scope":"project"}]`)
	put(t, filepath.Join(agent, "installed/package.json"), `{}`)
	return fixture{Config{PiBin: filepath.Join(root, "dist/cli.js"), AgentDir: agent}, root, []byte(settings)}
}
func check(t *testing.T, f fixture) Report {
	t.Helper()
	r, err := Check(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func noUpdatesRun(t *testing.T, f fixture) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(f.cfg.AgentDir, "calls.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an update command ran during check")
	}
}

func TestCheckUsesNativeAPIsReadOnlyGlobalOnly(t *testing.T) {
	f := newFixture(t)
	t.Setenv("GIT_DIR", "/should-not-be-used")
	t.Setenv("GIT_WORK_TREE", "/should-not-be-used")
	put(t, filepath.Join(f.root, "dist/utils/version-check.js"), `export async function checkForNewPiVersion(){if(process.env.GIT_DIR || process.env.GIT_WORK_TREE)throw 0;return {version:"1.2.4"};}`)
	// Project declarations are intentionally excluded, even if malformed.
	put(t, filepath.Join(filepath.Dir(f.cfg.AgentDir), ".pi/settings.json"), "SECRET malformed project settings")
	r := check(t, f)
	if r.CurrentVersion != "1.2.3" || r.LatestVersion != "1.2.4" || len(r.Packages) != 1 || r.Packages[0].Source != "npm:example" || !r.HasUpdates() {
		t.Fatalf("unexpected report: %+v", r)
	}
	got, _ := os.ReadFile(filepath.Join(f.cfg.AgentDir, "settings.json"))
	if !bytes.Equal(got, f.settings) {
		t.Fatal("settings changed")
	}
	noUpdatesRun(t, f)
}
func TestCheckBundledPiEntry(t *testing.T) {
	f := newFixture(t)
	put(t, filepath.Join(f.root, "package.json"), `{"name":"@earendil-works/pi-coding-agent","version":"1.2.3","type":"module","bin":{"pi":"dist/bundle/cli.js"}}`)
	bundle := filepath.Join(f.root, "dist/bundle/cli.js")
	put(t, bundle, fakeCLI)
	put(t, filepath.Join(f.root, "CHANGELOG.md"), "## [1.2.3]\n\nBundled Pi changes.\n")
	link := filepath.Join(t.TempDir(), "pi")
	if err := os.Symlink(bundle, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f.cfg.PiBin = link
	report := check(t, f)
	if report.CurrentVersion != "1.2.3" || report.LatestVersion != "1.2.4" || !report.HasUpdates() {
		t.Fatalf("bundled installation not checked: %+v", report)
	}
	if notes := LoadReleaseNotes(f.cfg); notes.Markdown != "Bundled Pi changes." {
		t.Fatal("bundled installation changelog not found")
	}
	noUpdatesRun(t, f)
	// Finding the right package is insufficient: the selected executable must
	// still be exactly the package's declared CLI, not another script inside it.
	f.cfg.PiBin = filepath.Join(f.root, "dist/cli.js")
	if report := check(t, f); report.Unsupported == "" {
		t.Fatal("undeclared executable was accepted")
	}
	noUpdatesRun(t, f)
}

func TestOfflineDoesNotResolveOrSpawn(t *testing.T) {
	t.Setenv("PATH", "")
	for _, value := range []string{"1", "true", "YES", "0", "false"} {
		t.Setenv("PI_OFFLINE", value)
		r, err := Check(context.Background(), Config{})
		if err != nil || !r.Offline || r.HasUpdates() {
			t.Fatalf("%+v %v", r, err)
		}
	}
	t.Setenv("PI_OFFLINE", "")
	r, err := Check(context.Background(), Config{Offline: true})
	if err != nil || !r.Offline {
		t.Fatal(r, err)
	}
}
func TestUnsupportedChecksRequireManualUpdates(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setup       func(*testing.T, *fixture)
		unsupported string
		version     string
	}{
		{
			name: "selected wrapper never runs",
			setup: func(t *testing.T, f *fixture) {
				f.cfg.PiBin = filepath.Join(t.TempDir(), "pi-wrapper")
				put(t, f.cfg.PiBin, "#!/bin/sh\nexit 99\n")
			},
		},
		{
			name: "unsupported native modules",
			setup: func(t *testing.T, f *fixture) {
				if err := os.Remove(filepath.Join(f.root, "dist/utils/version-check.js")); err != nil {
					t.Fatal(err)
				}
			},
			version: "1.2.3",
		},
		{
			name: "custom npm wrappers",
			setup: func(t *testing.T, f *fixture) {
				put(t, filepath.Join(f.cfg.AgentDir, "settings.json"), `{"packages":["npm:example"],"npmCommand":["npm","install"]}`)
			},
			unsupported: "Custom npm command wrappers require manual update checks.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.setup(t, &f)
			r := check(t, f)
			if r.Unsupported == "" || r.HasUpdates() {
				t.Fatal(r)
			}
			if tc.unsupported != "" && r.Unsupported != tc.unsupported {
				t.Fatal(r)
			}
			if tc.version != "" && r.CurrentVersion != tc.version {
				t.Fatal(r)
			}
			noUpdatesRun(t, f)
		})
	}
}

func TestNoReportedUpdatesIsNotAnError(t *testing.T) {
	f := newFixture(t)
	put(t, filepath.Join(f.root, "dist/utils/version-check.js"), `export async function checkForNewPiVersion(){return undefined;}`)
	put(t, filepath.Join(f.cfg.AgentDir, "available.json"), `[]`)
	r := check(t, f)
	if r.HasUpdates() {
		t.Fatal(r)
	}
}
func TestFailuresAndLargeOutputDoNotLeak(t *testing.T) {
	for _, script := range []string{
		`throw Error('SECRET https://user:pass@example.invalid')`,
		`process.stdout.write('SECRET'.repeat(300000));export async function checkForNewPiVersion(){return undefined;}`,
	} {
		t.Run(script[:5], func(t *testing.T) {
			f := newFixture(t)
			put(t, filepath.Join(f.root, "dist/utils/version-check.js"), script)
			r, err := Check(context.Background(), f.cfg)
			text := r.Unsupported
			if err != nil {
				text += err.Error()
			}
			if strings.Contains(text, "SECRET") || strings.Contains(text, "pass@") {
				t.Fatal(text)
			}
			if err == nil && r.Unsupported == "" {
				t.Fatal("expected failure")
			}
		})
	}
}
func TestCancellationIsBounded(t *testing.T) {
	f := newFixture(t)
	put(t, filepath.Join(f.root, "dist/utils/version-check.js"), `export async function checkForNewPiVersion(){await new Promise(r=>setTimeout(r,60000));}`)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Check(ctx, f.cfg)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
	noUpdatesRun(t, f)
}
