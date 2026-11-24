package workflow

import "testing"

// The minimal glob follows desktop's RelativeGlob: "*" and "?" stay inside a segment, "**" spans
// zero or more whole segments, and matching never normalizes the candidate path.
func TestCompletionFileMatches(t *testing.T) {
	for _, test := range []struct {
		pattern, path string
		want          bool
	}{
		{"reports/plan.md", "reports/plan.md", true},
		{"reports/plan.md", "reports/plan.mdx", false},
		{"reports/*.md", "reports/plan.md", true},
		{"reports/*.md", "reports/sub/plan.md", false},
		{"reports/?.md", "reports/a.md", true},
		{"reports/?.md", "reports/ab.md", false},
		{"reports/**/*.md", "reports/plan.md", true},
		{"reports/**/*.md", "reports/a/b/plan.md", true},
		{"**/*.md", "plan.md", true},
		{"**", "a/b/c", true},
		{"*.md", "a/plan.md", false},
		{"**/**/x", "x", true},
	} {
		file, err := NewCompletionFile(test.pattern, false)
		if err != nil {
			t.Fatalf("%q: %v", test.pattern, err)
		}
		if got := file.Matches(test.path); got != test.want {
			t.Errorf("%q matches %q = %v, want %v", test.pattern, test.path, got, test.want)
		}
	}
}

func TestCompletionGlobRefusals(t *testing.T) {
	for _, pattern := range []string{
		"", "/abs.md", "C:/x.md", `a\b.md`, "!a.md", "a/[x].md", "a/{x,y}.md", "a//b.md", "./a.md",
		"a/../b.md", "a**/b.md", "a/**b", "con.txt", "dir/LPT1", "dir/d:x.md", "nul .txt", "a\x01b", string(make([]byte, 513)),
	} {
		if _, reason := parseCompletionGlob(pattern); reason == "" {
			t.Errorf("pattern %q was accepted", pattern)
		}
	}
	for _, pattern := range []string{"plan.md", "reports/**/*.md", "a?.txt", "console.txt", "COM0", "notes:v2.md"} {
		if _, reason := parseCompletionGlob(pattern); reason != "" {
			t.Errorf("pattern %q was refused: %s", pattern, reason)
		}
	}
}

func TestAgentReadsCompletionSettings(t *testing.T) {
	n := Node{data: object{"agentConfig": object{
		"schemaVersion": 4, "interactive": true, "completionSignal": "agent", "autoAcceptAgentCompletion": true,
		"completion": object{"files": []any{object{"path": "out/*.md", "mustChange": true}, object{"path": "notes.md"}}},
	}}}
	cfg := n.Agent()
	if cfg.CompletionSignal != CompletionSignalAgent || !cfg.AutoAcceptAgentCompletion {
		t.Fatalf("policy = %q auto %v", cfg.CompletionSignal, cfg.AutoAcceptAgentCompletion)
	}
	if len(cfg.CompletionFiles) != 2 || cfg.CompletionFiles[0].Path != "out/*.md" || !cfg.CompletionFiles[0].MustChange || cfg.CompletionFiles[1].MustChange {
		t.Fatalf("files = %#v", cfg.CompletionFiles)
	}
	if (Node{data: object{"agentConfig": object{}}}).Agent().CompletionSignal != CompletionSignalEndTurn {
		t.Fatal("an absent policy must read as end_turn")
	}
}

// Only a node v2 parks waits for a report: an interactive Agent of the root graph, spare ones and
// container members aside.
func TestUsesAgentCompletionCountsOnlyParkedNodes(t *testing.T) {
	graph := func(agent string) string {
		return `{"nodes":[{"id":"start","data":{"kind":"start"}},` + agent + `],"edges":[{"source":"start","target":"a"}]}`
	}
	config := func(policy string) string {
		return `{"schemaVersion":4,"prompt":"x","executor":{"agentCli":"a","modelId":"m"}` + policy + `}`
	}
	agent := `{"id":"a","data":{"kind":"agent","agentConfig":`
	for name, tc := range map[string]struct {
		graph string
		want  bool
	}{
		"agent policy":    {graph(agent + config(`,"interactive":true,"completionSignal":"agent"`) + `}}`), true},
		"end_turn policy": {graph(agent + config(`,"interactive":true,"completionSignal":"end_turn"`) + `}}`), false},
		"not interactive": {graph(agent + config(`,"completionSignal":"agent"`) + `}}`), false},
		"spare": {graph(agent + config("") + `}},{"id":"spare","data":{"kind":"agent","agentConfig":` +
			config(`,"interactive":true,"completionSignal":"agent"`) + `}}`), false},
		"loop member": {`{"schemaVersion":2,"nodes":[{"id":"start","data":{"kind":"start"}},
			{"id":"loop","data":{"kind":"loop","loopConfig":{"maxIterations":2,"variables":[],"until":{"logic":"and","conditions":[]},"outputs":[]}}},
			{"id":"entry","parentId":"loop","data":{"kind":"start","containerId":"loop"}},
			{"id":"a","parentId":"loop","data":{"kind":"agent","containerId":"loop","agentConfig":` + config(`,"interactive":true,"completionSignal":"agent"`) + `}}],
			"edges":[{"source":"start","target":"loop"},{"source":"entry","target":"a"}]}`, false},
	} {
		g, err := ParseGraph(tc.graph)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := g.UsesAgentCompletion(); got != tc.want {
			t.Errorf("%s = %v, want %v", name, got, tc.want)
		}
	}
}
