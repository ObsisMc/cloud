package simulator

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Fixture commands must not leave maintenance processes writing after their owner returns.
func TestGitDoesNotStartAutomaticMaintenance(t *testing.T) {
	ctx := t.Context()
	repo := filepath.Join(t.TempDir(), "repo")
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	run := func(args ...string) {
		t.Helper()
		if _, err := git(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--initial-branch=main", repo)
	// Repository configuration must not opt a simulator-owned command into background work.
	run("-C", repo, "config", "maintenance.auto", "true")
	run("-C", repo, "config", "gc.auto", "1")
	cmd := gitCommand(ctx, "-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "base")
	cmd.Env = append(cmd.Env, "GIT_TRACE2_EVENT="+trace)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, output)
	}
	file, err := os.Open(trace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "child_start" {
			for _, arg := range event.Argv {
				if arg == "maintenance" || arg == "gc" {
					t.Fatalf("Git started automatic maintenance: %v", event.Argv)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}
