//go:build linux

package agentruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
)

func TestRegisteredRecoveryPreservesDataAndCleanupOnlyOwned(t *testing.T) {
	root, err := os.MkdirTemp("", "ar-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	p := &projectRuntime{id: "project", root: filepath.Join(root, digest("project")[:16])}
	if err := p.register(); err != nil {
		t.Fatal(err)
	}
	if err := Recover(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(p.root, "w", "r", "marker")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("keep checkout"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Recover(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep checkout" {
		t.Fatalf("recovery changed checkout: %q %v", data, err)
	}
	unknown := filepath.Join(root, "unknown")
	if err := os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(t.Context(), root); !errors.Is(err, execution.ErrUnavailable) {
		t.Fatalf("unknown ownership: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("cleanup partially deleted before validating ownership")
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned root not removed: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("removed parent directory")
	}
}

// TestLeftoverNodeHelper blocks when started as a Node an earlier process left behind.
func TestLeftoverNodeHelper(t *testing.T) {
	if os.Getenv("V2_LEFTOVER_NODE") != "" {
		time.Sleep(time.Minute)
	}
}

// New stops the Node a killed v2-server left running before it returns, which is what makes a
// restart replace every credential an Agent of that Node held (package completiontool).
func TestNewStopsTheNodeOfAnEarlierProcess(t *testing.T) {
	root, err := os.MkdirTemp("", "ar-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	p := &projectRuntime{id: "project", root: filepath.Join(root, digest("project")[:16])}
	if err := p.register(); err != nil {
		t.Fatal(err)
	}
	if err := Recover(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.root, "w", "nb")
	if err := os.WriteFile(path, binary, 0700); err != nil {
		t.Fatal(err)
	}
	leftover := exec.Command(path, "-test.run=^TestLeftoverNodeHelper$")
	leftover.Env = append(os.Environ(), "V2_LEFTOVER_NODE=1")
	leftover.Dir = filepath.Join(p.root, "w")
	if err := leftover.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = leftover.Wait(); close(exited) }()
	t.Cleanup(func() { _ = leftover.Process.Kill(); <-exited })
	driver, err := New(Options{Root: root, AssetsDirectory: "/assets", GitBackend: "/bin/false",
		Bindings:       map[string]nodeservices.Binding{"fixture": {Model: ipcModel(), Protocol: nodeservices.OpenAICompletions, Provider: nodeservices.FixtureProvider{}}},
		ResolveProject: func(context.Context, execution.Scope, string) (Project, error) { return Project{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("the earlier Node outlived New")
	}
	if err := driver.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRejectsSymlinksAndForgedMarker(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "foreign")); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(t.Context(), root); err == nil {
		t.Fatal("accepted symlink entry")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("affected symlink target")
	}
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	if err := Recover(t.Context(), alias); err == nil {
		t.Fatal("accepted symlink root")
	}
	root = t.TempDir()
	p := &projectRuntime{id: "project", root: filepath.Join(root, digest("project")[:16])}
	if err := p.register(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.root, markerName), []byte(`{"version":1,"project":"different"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(t.Context(), root); err == nil {
		t.Fatal("accepted mismatched registration")
	}
}

func TestNewFailsClosedOnUnknownRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "unowned"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{Root: root, AssetsDirectory: "/assets", GitBackend: "/git", Bindings: map[string]nodeservices.Binding{"fixture": {Model: nodeservices.Model{ID: "fixture"}, Protocol: nodeservices.OpenAICompletions, Provider: nodeservices.FixtureProvider{}}}, ResolveProject: func(context.Context, execution.Scope, string) (Project, error) { return Project{}, nil }})
	if err == nil {
		t.Fatal("New ignored unknown runtime ownership")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "unowned")); string(data) != "keep" {
		t.Fatal("New changed unknown file")
	}
}

func TestNewRejectsLongNodeSocketPathBeforeStarting(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("x", 80))
	_, err := New(Options{Root: root, AssetsDirectory: "/assets", GitBackend: "/git", Bindings: map[string]nodeservices.Binding{"fixture": {Model: nodeservices.Model{ID: "fixture"}, Protocol: nodeservices.OpenAICompletions, Provider: nodeservices.FixtureProvider{}}}, ResolveProject: func(context.Context, execution.Scope, string) (Project, error) { return Project{}, nil }})
	if err == nil || !strings.Contains(err.Error(), "V2_DATA_DIR") {
		t.Fatalf("missing actionable path error: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created runtime before rejecting path: %v", err)
	}
}
