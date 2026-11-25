package main

import (
	"os"
	"strings"
	"testing"
)

// The completion tool's credentials live in memory because startup stops earlier Nodes and ends
// their executions before the tool is served (package completiontool); this pins that order.
func TestTheCompletionToolIsServedAfterEarlierNodesStopped(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, step := range []string{"agentruntime.New(", "agentruntime.Recover(", "executionruntime.New(", "completiontool.Serve("} {
		at := strings.Index(string(source), step)
		if strings.Count(string(source), step) != 1 || at < last {
			t.Fatalf("%s is missing, repeated or out of order", step)
		}
		last = at
	}
}

// The tool is also refused to an execution whose end this process decided and has yet to write; the
// wiring has no test of its own beyond this one, which pins that the registry is given it.
func TestTheCompletionToolKnowsWhatThisProcessDecided(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(source), "completiontool.NewRegistry(db, runtime.HostExecutionEnding)") != 1 {
		t.Fatal("the completion tool is not given the ends this process decided")
	}
}
