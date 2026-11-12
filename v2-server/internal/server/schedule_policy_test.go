package server

import (
	"encoding/json"
	"testing"
)

func TestWorkflowVersionPolicyBoundary(t *testing.T) {
	const workflow = "33333333-3333-4333-8333-333333333333"
	const snapshot = "44444444-4444-4444-8444-444444444444"
	for _, tc := range []struct {
		name   string
		fields map[string]any
		valid  bool
	}{
		{"legacy fixed", map[string]any{"snapshotId": snapshot}, true},
		{"explicit fixed", map[string]any{"versionPolicy": "fixed", "snapshotId": snapshot}, true},
		{"latest", map[string]any{"versionPolicy": "latest"}, true},
		{"latest with pin", map[string]any{"versionPolicy": "latest", "snapshotId": snapshot}, false},
		{"unknown policy", map[string]any{"versionPolicy": "newest", "snapshotId": snapshot}, false},
		{"fixed without pin", map[string]any{"versionPolicy": "fixed"}, false},
		{"legacy without pin", map[string]any{}, false},
		{"wrong field type", map[string]any{"versionPolicy": true}, false},
		{"scope injection", map[string]any{"versionPolicy": "latest", "tenantId": workflow}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"kind": "workflow", "workflowId": workflow, "input": ""}
			for k, v := range tc.fields {
				fields[k] = v
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			_, code := decodeScheduleWorkflow(raw)
			if (code == "") != tc.valid {
				t.Fatalf("valid=%v, code=%q", tc.valid, code)
			}
		})
	}
}
