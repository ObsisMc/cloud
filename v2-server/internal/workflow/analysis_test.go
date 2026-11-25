package workflow_test

import (
	"reflect"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

// Analysis must report the very list a run records as `unapplied`, so the editor and the finished
// attempt cannot disagree. The expectation is read from AgentConfig.Unapplied rather than spelled
// out, because the set shrinks as v2 learns to apply settings; the analysis must follow without edits.
func TestAnalysisReportsUnappliedAgentSettings(t *testing.T) {
	configs := map[string]obj{
		"role":        {"roleId": "reviewer"},
		"mcp":         {"mcps": []any{obj{"mcpId": "official/tools", "enabled": true}}},
		"interactive": {"interactive": true},
		"retry":       {"retry": obj{"enabled": true, "maxRetries": 2, "initialDelaySeconds": 5}},
		"everything": {"roleId": "reviewer", "interactive": true, "mcps": []any{obj{"mcpId": "official/tools", "enabled": true}},
			"retry": obj{"enabled": true, "maxRetries": 2, "initialDelaySeconds": 5}},
		"disabled mcp":        {"mcps": []any{obj{"mcpId": "official/tools", "enabled": false}}},
		"disabled retry":      {"retry": obj{"enabled": false, "maxRetries": 2, "initialDelaySeconds": 5}},
		"blank role":          {"roleId": "  "},
		"plain":               {"prompt": "x"},
		"agent policy":        {"schemaVersion": 4, "interactive": true, "completionSignal": "agent", "autoAcceptAgentCompletion": true},
		"wait inactivity":     {"promptInactivity": "wait"},
		"timeout inactivity":  {"promptInactivity": "timeout"},
		"agent policy + wait": {"schemaVersion": 4, "interactive": true, "completionSignal": "agent", "autoAcceptAgentCompletion": true, "promptInactivity": "wait"},
		"end_turn policy": {"schemaVersion": 4, "interactive": true, "completionSignal": "end_turn",
			"completion": obj{"files": []any{obj{"path": "plan.md"}}}},
	}
	g := flat()
	for id, config := range configs {
		config["prompt"] = "x"
		if _, set := config["retry"]; !set {
			// An absent policy is desktop's default retry, which v2 lists; switch it off to isolate the rest.
			config["retry"] = obj{"enabled": false, "maxRetries": 2, "initialDelaySeconds": 5}
		}
		addNode(g, id, "agent", obj{"agentConfig": config})
		connect(g, "start", id)
	}
	// A spare Agent is still edited in the inspector, so its settings are explained as well.
	addNode(g, "spare", "agent", obj{"agentConfig": obj{"roleId": "reviewer"}})
	// A Loop member is the one placement that still records a setting: v2 runs an interactive member
	// automatically where desktop parks it for its starter, and the member's container is part of the
	// placement both sides read. Roles, Skills, MCP and retry all apply now, so without it the fixture
	// below would list nothing and compare two empty maps.
	g["schemaVersion"] = 2 // a container graph needs it
	g["nodes"] = append(g["nodes"].([]any),
		obj{"id": "loop", "data": obj{"kind": "loop", "loopConfig": obj{"maxIterations": 3, "variables": []any{},
			"until": obj{"logic": "and", "conditions": []any{}}, "outputs": []any{}}}},
		obj{"id": "entry", "parentId": "loop", "data": obj{"kind": "start", "containerId": "loop"}},
		obj{"id": "loop-interactive", "parentId": "loop", "data": obj{"kind": "agent", "containerId": "loop",
			"agentConfig": obj{"prompt": "x", "interactive": true}}},
	)
	connect(g, "start", "loop")
	connect(g, "entry", "loop-interactive")
	source := encode(t, g)
	graph, err := workflow.ParseGraph(source)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := workflow.AnalyzeWorkflow(source)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, entry := range analysis.UnappliedSettings {
		if _, repeated := got[entry.NodeID]; repeated || len(entry.Settings) == 0 {
			t.Fatalf("node %s listed twice or with no settings: %#v", entry.NodeID, analysis.UnappliedSettings)
		}
		got[entry.NodeID] = entry.Settings
	}
	want := map[string][]string{}
	for id := range configs {
		node, _ := graph.Node(id)
		if unapplied := node.Agent().Unapplied; len(unapplied) != 0 {
			want[id] = unapplied
		}
	}
	// ParseGraph drops spare nodes; read the spare Agent's settings from a graph where it is wired.
	connect(g, "start", "spare")
	wired, err := workflow.ParseGraph(encode(t, g))
	if err != nil {
		t.Fatal(err)
	}
	if node, _ := wired.Node("spare"); len(node.Agent().Unapplied) != 0 {
		want["spare"] = node.Agent().Unapplied
	}
	if node := runPathView(t, graph, "loop-interactive"); len(node.Agent().Unapplied) != 0 {
		want["loop-interactive"] = node.Agent().Unapplied
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unapplied settings = %v, want the run's own list %v", got, want)
	}
	if len(want) == 0 {
		t.Fatal("no setting is unapplied any more; replace this fixture with one that still is")
	}
	if want := []string{"promptInactivity"}; !reflect.DeepEqual(got["wait inactivity"], want) {
		t.Errorf("wait inactivity unapplied = %v, want %v", got["wait inactivity"], want)
	}
	if want := []string{"promptInactivity"}; !reflect.DeepEqual(got["agent policy + wait"], want) {
		t.Errorf("agent policy + wait unapplied = %v, want %v", got["agent policy + wait"], want)
	}
	for _, id := range []string{"agent policy", "disabled mcp", "disabled retry", "blank role", "plain", "end_turn policy", "timeout inactivity"} {
		if _, listed := got[id]; listed {
			t.Errorf("node %q configures nothing the run skips, but analysis lists %v", id, got[id])
		}
	}
}

// Start refuses reachable prompt and tool nodes; analysis names exactly those, so the start dialog
// warns about what start will refuse and stays quiet about spare nodes.
func TestAnalysisReportsReachableUnsupportedNodes(t *testing.T) {
	loop := func(bodyKind string, wired bool) obj {
		edges := []any{obj{"source": "entry", "target": "body"}}
		if wired {
			edges = append(edges, obj{"source": "start", "target": "loop"})
		}
		return obj{"schemaVersion": 2, "nodes": []any{
			obj{"id": "start", "data": obj{"kind": "start"}},
			obj{"id": "loop", "data": obj{"kind": "loop"}},
			obj{"id": "entry", "parentId": "loop", "data": obj{"kind": "start", "containerId": "loop"}},
			obj{"id": "body", "parentId": "loop", "data": obj{"kind": bodyKind, "containerId": "loop"}},
		}, "edges": edges}
	}
	for name, tc := range map[string]struct {
		graph obj
		want  []workflow.UnsupportedNode
	}{
		"reachable prompt and tool": {func() obj {
			g := flat()
			addNode(g, "p", "prompt", nil)
			addNode(g, "a", "agent", nil)
			addNode(g, "t", "tool", nil)
			connect(g, "start", "p")
			connect(g, "start", "a")
			connect(g, "a", "t")
			return g
		}(), []workflow.UnsupportedNode{{NodeID: "p", Kind: "prompt"}, {NodeID: "t", Kind: "tool"}}},
		"spare prompt and tool": {func() obj {
			g := flat()
			addNode(g, "p", "prompt", nil)
			addNode(g, "t", "tool", nil)
			connect(g, "p", "t")
			return g
		}(), []workflow.UnsupportedNode{}},
		"no start": {obj{"nodes": []any{obj{"id": "t", "data": obj{"kind": "tool"}}}, "edges": []any{}},
			[]workflow.UnsupportedNode{}},
		"unrecognized kind is not unsupported": {func() obj {
			g := flat()
			addNode(g, "h", "hologram", nil)
			connect(g, "start", "h")
			return g
		}(), []workflow.UnsupportedNode{}},
		"supported kinds":          {fixture(t, "loop"), []workflow.UnsupportedNode{}},
		"tool in a reachable Loop": {loop("tool", true), []workflow.UnsupportedNode{{NodeID: "body", Kind: "tool"}}},
		"tool in a spare Loop":     {loop("tool", false), []workflow.UnsupportedNode{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			analysis, err := workflow.AnalyzeWorkflow(encode(t, tc.graph))
			if err != nil || !reflect.DeepEqual(analysis.UnsupportedNodes, tc.want) {
				t.Fatalf("unsupported nodes = %#v, %v; want %#v", analysis.UnsupportedNodes, err, tc.want)
			}
		})
	}
}
