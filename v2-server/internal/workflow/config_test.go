package workflow_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

func withExecutor(agentConfig obj) obj {
	out := obj{"executor": obj{"agentCli": "claude", "modelId": "model"}}
	for k, v := range agentConfig {
		out[k] = v
	}
	return out
}

// These acceptance/rejection pairs exercise the public publication boundary,
// following desktop start_input, agent_config, retry and structured_output tests.
func TestNodeConfigurationValidation(t *testing.T) {
	tests := []struct {
		name, kind string
		data       obj
		valid      bool
	}{
		{"missing agent config", "agent", obj{}, false},
		{"missing executor", "agent", obj{"agentConfig": obj{"prompt": ""}}, false},
		{"empty agentCli", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "", "modelId": "model"}}}, false},
		{"whitespace agentCli", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "  ", "modelId": "model"}}}, false},
		{"empty modelId", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "claude", "modelId": ""}}}, false},
		{"whitespace modelId", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "claude", "modelId": " \t"}}}, false},
		{"no local catalog required", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "not-installed", "modelId": "not-installed"}, "prompt": ""}}, true},
		{"agent not object", "agent", obj{"agentConfig": false}, false},
		{"wrong prompt", "agent", obj{"agentConfig": withExecutor(obj{"prompt": 2})}, false},
		{"wrong executor", "agent", obj{"agentConfig": obj{"executor": "bad"}}, false},
		{"wrong interactive", "agent", obj{"agentConfig": withExecutor(obj{"interactive": "true"})}, false},
		{"bad skills shape", "agent", obj{"agentConfig": withExecutor(obj{"skills": nil})}, false},
		{"legacy skill", "agent", obj{"agentConfig": withExecutor(obj{"skills": []any{obj{}}})}, true},
		{"valid mcp", "agent", obj{"agentConfig": withExecutor(obj{"mcps": []any{obj{"mcpId": "official/tools", "enabled": true}}})}, true},
		{"noncanonical mcp", "agent", obj{"agentConfig": withExecutor(obj{"mcps": []any{obj{"mcpId": "tools", "enabled": true}}})}, false},
		{"mcp namespace limit", "agent", obj{"agentConfig": withExecutor(obj{"mcps": []any{obj{"mcpId": strings.Repeat("a", 34) + "/tools", "enabled": false}}})}, false},
		{"duplicate disabled mcp", "agent", obj{"agentConfig": withExecutor(obj{"mcps": []any{obj{"mcpId": "local/tools", "enabled": false}, obj{"mcpId": "local/tools", "enabled": false}}})}, false},
		{"mcp enabled required", "agent", obj{"agentConfig": withExecutor(obj{"mcps": []any{obj{"mcpId": "local/tools"}}})}, false},
		{"wait inactivity", "agent", obj{"agentConfig": withExecutor(obj{"promptInactivity": "wait"})}, true},
		{"unknown inactivity", "agent", obj{"agentConfig": withExecutor(obj{"promptInactivity": "forever"})}, false},
		{"retry limits", "agent", obj{"agentConfig": withExecutor(obj{"retry": obj{"enabled": true, "maxRetries": 5, "initialDelaySeconds": 300}})}, true},
		{"disabled retry still validated", "agent", obj{"agentConfig": withExecutor(obj{"retry": obj{"enabled": false, "maxRetries": 6, "initialDelaySeconds": 10}})}, false},
		{"retry missing field", "agent", obj{"agentConfig": withExecutor(obj{"retry": obj{"enabled": true, "maxRetries": 2}})}, false},
		{"retry fraction", "agent", obj{"agentConfig": withExecutor(obj{"retry": obj{"enabled": true, "maxRetries": 1.5, "initialDelaySeconds": 10}})}, false},
		{"retry negative", "agent", obj{"agentConfig": withExecutor(obj{"retry": obj{"enabled": true, "maxRetries": 2, "initialDelaySeconds": -1}})}, false},
		{"empty condition shell", "condition", obj{}, true},
		{"condition missing id", "condition", obj{"cases": []any{obj{"conditions": []any{}}}}, false},
		{"condition empty id is parseable", "condition", obj{"cases": []any{obj{"id": ""}}}, true},
		{"condition duplicate id is start check", "condition", obj{"cases": []any{obj{"id": "same"}, obj{"id": "same"}}}, true},
		{"condition wrong logic", "condition", obj{"cases": []any{obj{"id": "a", "logic": "xor"}}}, false},
		{"condition unknown operator", "condition", obj{"cases": []any{obj{"id": "a", "conditions": []any{obj{"variableSelector": []string{"start", "input"}, "operator": "regex"}}}}}, false},
		{"condition incomplete selector", "condition", obj{"cases": []any{obj{"id": "a", "conditions": []any{obj{"variableSelector": []string{"start"}, "operator": "exists"}}}}}, false},
		{"condition unknown producer is runtime lookup", "condition", obj{"cases": []any{obj{"id": "a", "conditions": []any{obj{"variableSelector": []string{"external", "value"}, "operator": "exists"}}}}}, true},
		{"output legacy rows", "output", obj{"outputs": []any{obj{}, obj{"name": "x", "variableSelector": []string{"start"}}}}, true},
		{"output wrong name type", "output", obj{"outputs": []any{obj{"name": false}}}, false},
		{"nullable output selector rejected", "output", obj{"outputs": []any{obj{"name": "x", "variableSelector": nil}}}, false},
		{"aggregator needs config", "aggregator", obj{}, false},
		{"aggregator needs selectors", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{}}}, false},
		{"aggregator root selector", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{[]string{"start", "input"}}}}, true},
		{"aggregator nested selector", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{[]string{"start", "input", "nested"}}}}, false},
		{"aggregator duplicate", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{[]string{"start", "input"}, []string{"start", "input"}}}}, false},
		{"aggregator unknown variable", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{[]string{"start", "unknown"}}}}, false},
		{"bad unrelated wire field", "output", obj{"cases": "unfinished"}, false},
		{"agent schema version 3", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 3})}, true},
		{"agent schema version 5", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 5})}, false},
		{"agent schema version text", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": "4"})}, false},
		{"completion signal needs version 4", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 3, "completionSignal": "end_turn"})}, false},
		{"completion signal without version", "agent", obj{"agentConfig": withExecutor(obj{"completionSignal": "agent"})}, false},
		{"completion signal end_turn", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "interactive": true, "completionSignal": "end_turn"})}, true},
		{"completion signal agent", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "interactive": true, "completionSignal": "agent", "autoAcceptAgentCompletion": true})}, true},
		{"completion signal unknown", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completionSignal": "tool"})}, false},
		{"auto accept not boolean", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "autoAcceptAgentCompletion": "yes"})}, false},
		{"auto accept needs version 4", "agent", obj{"agentConfig": withExecutor(obj{"autoAcceptAgentCompletion": false})}, false},
		{"completion files", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "reports/plan.md", "mustChange": true}, obj{"path": "docs/**/*.md"}}}})}, true},
		{"completion files need version 4", "agent", obj{"agentConfig": withExecutor(obj{"completion": obj{"files": []any{obj{"path": "a.md"}}}})}, false},
		{"completion files empty", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{}}})}, false},
		{"completion files missing", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{}})}, false},
		{"completion unknown field", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "a.md"}}, "criteria": "x"}})}, false},
		{"completion file unknown field", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "a.md", "checklist": true}}}})}, false},
		{"completion file without path", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"mustChange": true}}}})}, false},
		{"completion file null mustChange", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "a.md", "mustChange": nil}}}})}, false},
		{"completion file absolute", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "/etc/passwd"}}}})}, false},
		{"completion file traversal", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "a/../b.md"}}}})}, false},
		{"completion file class", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": []any{obj{"path": "a/[ab].md"}}}})}, false},
		{"completion too many files", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": manyFiles(33)}})}, false},
		{"completion 32 files", "agent", obj{"agentConfig": withExecutor(obj{"schemaVersion": 4, "completion": obj{"files": manyFiles(32)}})}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			g := flat()
			addNode(g, "target", test.kind, test.data)
			connect(g, "start", "target")
			if test.valid {
				requireValid(t, g)
			} else {
				requireInvalid(t, g, "")
			}
		})
	}
}

func TestStartDeclarations(t *testing.T) {
	for _, test := range []struct {
		name     string
		variable obj
		valid    bool
	}{
		{"required without default", obj{"name": "x", "valueType": "string", "required": true}, true},
		{"empty name", obj{"name": " ", "valueType": "string"}, false},
		{"dot name", obj{"name": "x.y", "valueType": "string"}, false},
		{"unknown type", obj{"name": "x", "valueType": "decimal"}, false},
		{"typed array", obj{"name": "x", "valueType": "array[string]", "fieldType": "json", "value": []string{"a", "b"}}, true},
		{"mixed typed array", obj{"name": "x", "valueType": "array[string]", "value": []any{"a", 0}}, false},
		{"legacy integer", obj{"name": "x", "valueType": "integer", "value": 1}, true},
		{"integer negative zero", obj{"name": "x", "valueType": "integer", "value": json.Number("-0")}, false},
		{"integer minimum", obj{"name": "x", "valueType": "integer", "value": json.Number("-9223372036854775808")}, true},
		{"integer maximum", obj{"name": "x", "valueType": "integer", "value": json.Number("18446744073709551615")}, true},
		{"integer overflow", obj{"name": "x", "valueType": "integer", "value": json.Number("18446744073709551616")}, false},
		{"explicit number integer mismatch", obj{"name": "x", "valueType": "integer", "fieldType": "number"}, false},
		{"unicode length", obj{"name": "x", "valueType": "string", "maxLength": 2, "value": "中文"}, true},
		{"too long", obj{"name": "x", "valueType": "string", "maxLength": 2, "value": "中文三"}, false},
		{"zero length", obj{"name": "x", "valueType": "string", "maxLength": 0}, false},
		{"nontext length", obj{"name": "x", "valueType": "number", "maxLength": 4}, false},
		{"select", obj{"name": "x", "valueType": "string", "fieldType": "select", "options": []string{" a ", "b"}, "value": "a"}, true},
		{"duplicate trimmed option", obj{"name": "x", "valueType": "string", "fieldType": "select", "options": []string{" a ", "a"}}, false},
		{"select wrong default", obj{"name": "x", "valueType": "string", "fieldType": "select", "options": []string{"a"}, "value": "b"}, false},
		{"options on number", obj{"name": "x", "valueType": "number", "options": []string{"a"}}, false},
		{"checkbox zero", obj{"name": "x", "valueType": "boolean", "fieldType": "checkbox", "value": 0}, false},
		{"checkbox false", obj{"name": "x", "valueType": "boolean", "fieldType": "checkbox", "value": false}, true},
		{"null means unset", obj{"name": "x", "valueType": "number", "value": nil}, true},
		{"legacy file", obj{"name": "x", "valueType": "file", "value": "docs\\./file.txt"}, true},
		{"file root", obj{"name": "x", "valueType": "file", "value": "./"}, false},
		{"file traversal", obj{"name": "x", "valueType": "file", "value": "docs/../file"}, false},
		{"file drive", obj{"name": "x", "valueType": "file", "value": "safe/C:secret"}, false},
		{"file reserved", obj{"name": "x", "valueType": "array[file]", "value": []string{"safe.txt", "docs/NuL.md"}}, false},
		{"canonical file", obj{"name": "x", "valueType": "file", "value": obj{"kind": "workspace_file", "path": "safe.txt"}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := flat()
			dataOf(g, "start")["inputVariables"] = []any{test.variable}
			if test.valid {
				requireValid(t, g)
			} else {
				requireInvalid(t, g, "")
			}
		})
	}
	g := flat()
	dataOf(g, "start")["inputVariables"] = []any{obj{"name": "x", "valueType": "string"}, obj{"name": " x ", "valueType": "string"}}
	requireInvalid(t, g, "")
}

func TestStructuredSchemaAdmission(t *testing.T) {
	for _, test := range []struct {
		name   string
		schema any
		valid  bool
	}{
		{"object", obj{"type": "object", "properties": obj{"result": obj{"type": "array", "items": obj{"type": "string"}}}, "required": []string{"result"}, "additionalProperties": false}, true},
		{"unknown extensions retained", obj{"type": "object", "description": "{{#unused.output#}}", "future": true}, true},
		{"root array", obj{"type": "array"}, false},
		{"missing schema", nil, false},
		{"nonobject schema", false, false},
		{"missing type", obj{}, false},
		{"unsupported property type", obj{"type": "object", "properties": obj{"x": obj{"type": "undefined"}}}, false},
		{"unknown required", obj{"type": "object", "required": []string{"missing"}}, false},
		{"required not array", obj{"type": "object", "required": "x"}, false},
		{"additional not boolean", obj{"type": "object", "additionalProperties": obj{}}, false},
		{"items on object", obj{"type": "object", "items": obj{"type": "string"}}, false},
		{"properties on scalar", obj{"type": "object", "properties": obj{"x": obj{"type": "string", "properties": obj{}}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := flat()
			addNode(g, "agent", "agent", obj{"agentConfig": withExecutor(obj{"outputContract": obj{"type": "structured", "schema": test.schema}})})
			connect(g, "start", "agent")
			if test.valid {
				requireValid(t, g)
			} else {
				requireInvalid(t, g, "")
			}
		})
	}
}

func TestGlobalAndDerivedTypes(t *testing.T) {
	for _, test := range []struct {
		name    string
		globals any
		valid   bool
	}{
		{"valid", []any{obj{"name": "global.flag", "valueType": "boolean", "value": false}, obj{"name": "global.zero", "valueType": "number", "value": 0}}, true},
		{"no value", []any{obj{"name": "global.x", "valueType": "any"}}, false},
		{"null is not assigned default", []any{obj{"name": "global.x", "valueType": "any", "value": nil}}, false},
		{"unqualified", []any{obj{"name": "x", "valueType": "string", "value": ""}}, false},
		{"wrong value", []any{obj{"name": "global.x", "valueType": "number", "value": "1"}}, false},
		{"duplicate", []any{obj{"name": "global.x", "valueType": "string", "value": "a"}, obj{"name": "global.x", "valueType": "string", "value": "b"}}, false},
		{"system defaults", []any{obj{"name": "sys.workflow_id", "valueType": "string"}, obj{"name": "sys.timestamp", "valueType": "number"}}, true},
		{"null list", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := flat()
			g["globalVariables"] = test.globals
			if test.valid {
				requireValid(t, g)
			} else {
				requireInvalid(t, g, "")
			}
		})
	}
	g := flat()
	dataOf(g, "start")["inputVariables"] = []any{obj{"name": "count", "valueType": "number"}}
	addNode(g, "agg", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{[]string{"start", "input"}, []string{"start", "count"}}}})
	connect(g, "start", "agg")
	requireInvalid(t, g, "invalid_aggregator")
	// A declared downstream producer still cannot supply an aggregator.
	dataOf(g, "agg")["aggregatorConfig"] = obj{"variables": []any{[]string{"later", "output"}}}
	addNode(g, "later", "agent", nil)
	connect(g, "agg", "later")
	requireInvalid(t, g, "invalid_aggregator")
	// Cycles reachable from Start are rejected, while the spare fixture's cycle is ignored.
	connect(g, "later", "start")
	requireInvalid(t, g, "cycle_detected")
	if _, err := workflow.AnalyzeWorkflow(encode(t, g)); err != nil {
		t.Fatalf("analysis must not perform DAG execution validation: %v", err)
	}
}

func TestDerivedCatalogKeepsFirstDeclaration(t *testing.T) {
	g := flat()
	dataOf(g, "start")["inputVariables"] = []any{obj{"name": "input", "valueType": "number"}}
	addNode(g, "agent", "agent", obj{"agentConfig": withExecutor(nil)})
	addNode(g, "agg", "aggregator", obj{"aggregatorConfig": obj{"variables": []any{[]string{"start", "input"}, []string{"agent", "output"}}}})
	connect(g, "start", "agent")
	connect(g, "agent", "agg")
	// The reserved Start input was declared as string before the user input.
	requireValid(t, g)
	// Globals precede both base and derived node declarations, as on desktop.
	g["globalVariables"] = []any{obj{"name": "agent.output", "valueType": "number", "value": 0}}
	requireInvalid(t, g, "invalid_aggregator")
}

// manyFiles declares n distinct completion files.
func manyFiles(n int) []any {
	files := make([]any, n)
	for i := range files {
		files[i] = obj{"path": "out/" + strings.Repeat("f", i+1) + ".md"}
	}
	return files
}

func TestPublishValidationRequiresAgentExecutorAndIdentifiesNode(t *testing.T) {
	for name, tc := range map[string]struct {
		graph     func() obj
		wantNode  string
		wantField string
	}{
		"root missing executor": {
			graph: func() obj {
				g := flat()
				addNode(g, "worker", "agent", obj{"agentConfig": obj{"prompt": "hi"}})
				connect(g, "start", "worker")
				return g
			},
			wantNode:  "worker",
			wantField: "data.agentConfig.executor",
		},
		"root empty agentCli": {
			graph: func() obj {
				g := flat()
				addNode(g, "worker", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "", "modelId": "fixture"}}})
				connect(g, "start", "worker")
				return g
			},
			wantNode:  "worker",
			wantField: "data.agentConfig.executor.agentCli",
		},
		"root empty modelId": {
			graph: func() obj {
				g := flat()
				addNode(g, "worker", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "claude", "modelId": "  "}}})
				connect(g, "start", "worker")
				return g
			},
			wantNode:  "worker",
			wantField: "data.agentConfig.executor.modelId",
		},
		"loop member empty executor": {
			graph: func() obj {
				g := fixture(t, "loop")
				delete(dataOf(g, "writer")["agentConfig"].(obj), "executor")
				return g
			},
			wantNode:  "writer",
			wantField: "data.agentConfig.executor",
		},
		"iteration member empty executor": {
			graph: func() obj {
				g := fixture(t, "iteration")
				dataOf(g, "fix")["agentConfig"] = obj{"executor": obj{"agentCli": "claude", "modelId": ""}}
				return g
			},
			wantNode:  "fix",
			wantField: "data.agentConfig.executor.modelId",
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := encode(t, tc.graph())
			err := workflow.ValidateWorkflow(source)
			var domain *workflow.ValidationError
			if !errors.As(err, &domain) || domain.Code != "invalid_node" || domain.NodeID != tc.wantNode || domain.Field != tc.wantField || domain.Reason == "" {
				t.Fatalf("ValidateWorkflow = %#v, want ValidationError{invalid_node, %q, %q}", err, tc.wantNode, tc.wantField)
			}
			// ParseGraph keeps legacy published snapshots readable without an executor.
			if _, err := workflow.ParseGraph(source); err != nil {
				t.Fatalf("ParseGraph rejected legacy snapshot: %v", err)
			}
			// ValidateImportedWorkflow allows marketplace workflow plugins without an executor.
			if err := workflow.ValidateImportedWorkflow(source); err != nil {
				t.Fatalf("ValidateImportedWorkflow rejected executorless template: %v", err)
			}
		})
	}

	// Spare Agents are not reachable from Start and may remain unfinished on publish.
	g := flat()
	addNode(g, "spare", "agent", obj{"agentConfig": obj{"executor": obj{"agentCli": "", "modelId": ""}}})
	requireValid(t, g)

	// ValidateImportedWorkflow still rejects non-executor configuration defects.
	invalidImport := flat()
	addNode(invalidImport, "worker", "agent", obj{"agentConfig": obj{"promptInactivity": "forever"}})
	connect(invalidImport, "start", "worker")
	if err := workflow.ValidateImportedWorkflow(encode(t, invalidImport)); err == nil {
		t.Fatal("ValidateImportedWorkflow accepted invalid promptInactivity")
	}
}
