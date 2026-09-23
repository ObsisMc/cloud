package collab

import (
	"context"
	"testing"

	"github.com/wanglongan587/cloud/internal/core"
)

// TestWireDevelopmentFixturesInstallsAllPorts pins the single composition point: enabling the
// development fixtures must make every collaboration port (Directory/Context/Dispatcher/Forms/Assist)
// available at once — a half-wired store would surface targets the runtime cannot actually serve.
func TestWireDevelopmentFixturesInstallsAllPorts(t *testing.T) {
	store := &core.Store{}
	WireDevelopmentFixtures(store)
	if store.Directory == nil || store.Context == nil || store.Dispatcher == nil || store.Forms == nil || store.Assist == nil {
		t.Fatalf("WireDevelopmentFixtures must install all five collaboration ports")
	}
}

// TestFixtureDirectoryAllNonHumanTargetTypesDiscoverable verifies that the fixture directory serves
// agent + team + workflow through the same target discovery interface the @ picker uses, with stable
// IDs and interaction descriptors consistent with the frozen target-type defaults, and never users.
func TestFixtureDirectoryAllNonHumanTargetTypesDiscoverable(t *testing.T) {
	var d core.CollaborationDirectory = FixtureCollaborationDirectory{}
	targets, err := d.ListTargets(context.Background(), "", "")
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	// Frozen target-type -> interaction-mode default (§37.2): user=mention, agent/team=task (task
	// required), workflow=form. The fixture descriptors must agree with it.
	expected := map[string][2]any{
		"user":     {"mention", false},
		"agent":    {"task", true},
		"team":     {"task", true},
		"workflow": {"form", false},
	}
	byType := map[string]int{}
	for _, s := range targets {
		byType[s.Type]++
		wantMode, wantTask := expected[s.Type][0].(string), expected[s.Type][1].(bool)
		if s.InteractionDescriptor.Mode != wantMode || s.InteractionDescriptor.RequiresTask != wantTask {
			t.Fatalf("descriptor for %s %q disagrees with frozen default (want mode=%s requiresTask=%v, got mode=%s requiresTask=%v)",
				s.Type, s.DisplayName, wantMode, wantTask, s.InteractionDescriptor.Mode, s.InteractionDescriptor.RequiresTask)
		}
	}
	if byType["agent"] != 2 || byType["team"] != 1 || byType["workflow"] != 2 {
		t.Fatalf("unexpected fixture target mix (want 2 agent/1 team/2 workflow): %v", byType)
	}
	if byType["user"] != 0 {
		t.Fatalf("humans must never come from the directory: %v", byType)
	}

	// Resolve each stable fixture ID through the same seam.
	for _, s := range targets {
		if _, ok, err := d.ResolveTarget(context.Background(), "", s.Type, s.ID); err != nil || !ok {
			t.Fatalf("ResolveTarget(%s, %s) = ok=%v err=%v", s.Type, s.ID, ok, err)
		}
	}
}
