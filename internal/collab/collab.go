// Package collab provides dev/demo implementations of the Issue collaboration ports: an in-memory
// fixture directory (agent/team/workflow), a deterministic ContextBuilder (no AI), and a mock
// ExecutionDispatcher that drives the observer lifecycle to completion synchronously.
//
// These are fixtures only — no database tables, no sim_* tables, no mock domain tables, no real
// Agent/Team/Workflow backend. They are wired off by default in production; cmd/ora-web and the
// integration suite opt in.
package collab

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wanglongan587/cloud/internal/core"
)

// Stable fixture identities (valid v4 UUIDs) so demo/dev data is reproducible across restarts.
const (
	BackendAgentID           = "11111111-1111-4111-8111-111111111111"
	ReviewAgentID            = "22222222-2222-4222-8222-222222222222"
	PlatformTeamID           = "33333333-3333-4333-8333-333333333333"
	SecurityReviewWorkflowID = "44444444-4444-4444-8444-444444444444"
	ReleaseWorkflowID        = "55555555-5555-4555-8555-555555555555"
	// SecurityReviewFormRef / ReleaseFormRef are the opaque formRefs the workflow targets advertise.
	// Issues never parses them.
	SecurityReviewFormRef = "security-review"
	ReleaseFormRef        = "release-readiness"
)

// FixtureCollaborationDirectory resolves agent/team/workflow targets from a fixed in-memory catalog.
// Human targets never come from here (they are derived from real tenant memberships).
type FixtureCollaborationDirectory struct{}

var fixtureTargets = []core.CollaborationTargetSummary{
	{Type: "agent", ID: BackendAgentID, DisplayName: "Backend Agent", Description: "Demo backend execution agent", InteractionDescriptor: core.InteractionDescriptor{Mode: "task", RequiresTask: true}},
	{Type: "agent", ID: ReviewAgentID, DisplayName: "Review Agent", Description: "Demo code-review agent", InteractionDescriptor: core.InteractionDescriptor{Mode: "task", RequiresTask: true}},
	{Type: "team", ID: PlatformTeamID, DisplayName: "Platform Team", Description: "Demo platform team", InteractionDescriptor: core.InteractionDescriptor{Mode: "task", RequiresTask: true}},
	{Type: "workflow", ID: SecurityReviewWorkflowID, DisplayName: "Security Review Workflow", Description: "Demo workflow (Form Mode)", InteractionDescriptor: core.InteractionDescriptor{Mode: "form", RequiresTask: false, FormRef: SecurityReviewFormRef}},
	{Type: "workflow", ID: ReleaseWorkflowID, DisplayName: "Release Readiness Workflow", Description: "Demo workflow (Form Mode; exercises number + multi_select)", InteractionDescriptor: core.InteractionDescriptor{Mode: "form", RequiresTask: false, FormRef: ReleaseFormRef}},
}

// FixtureFormDescriptorProvider serves the demo FormDescriptor for the Security Review fixture. The
// field set below exists ONLY here: the frontend renders whatever the descriptor declares and must
// never branch on a workflow id (§38.27, §38.33). It is not a real Workflow schema.
type FixtureFormDescriptorProvider struct{}

func (FixtureFormDescriptorProvider) ResolveFormDescriptor(_ context.Context, _, formRef string) (core.FormDescriptor, bool, error) {
	if formRef == ReleaseFormRef {
		return releaseDescriptor(), true, nil
	}
	if formRef != SecurityReviewFormRef {
		return core.FormDescriptor{}, false, nil
	}
	return core.FormDescriptor{
		FormRef:     SecurityReviewFormRef,
		Title:       "Security Review",
		Description: "Demo configuration for the security review workflow (fixture only — not a real Workflow schema).",
		Fields: []core.FormField{
			{Key: "repository", Label: "Repository", Type: "text", Required: true, Placeholder: "owner/name", Description: "Repository to review."},
			{Key: "branch", Label: "Branch", Type: "text", Required: false, DefaultValue: "main"},
			{
				Key: "scope", Label: "Review scope", Type: "select", Required: true,
				Options: []core.FormOption{
					{Value: "current-issue", Label: "Current issue changes"},
					{Value: "changed-files", Label: "Changed files"},
					{Value: "full-repo", Label: "Full repository"},
				},
			},
			{
				Key: "severity", Label: "Severity", Type: "select", Required: true,
				Options: []core.FormOption{
					{Value: "low", Label: "Low"},
					{Value: "medium", Label: "Medium"},
					{Value: "high", Label: "High"},
				},
			},
			{Key: "includeDependencies", Label: "Include dependencies", Type: "boolean", Required: false, DefaultValue: false},
			{Key: "additionalInstructions", Label: "Additional instructions", Type: "textarea", Required: false, Placeholder: "Anything else the reviewer should know…"},
		},
	}, true, nil
}

// releaseDescriptor is the second demo workflow. It exists to exercise the field types the security
// fixture does not (`number`, `multi_select`) and to prove the frontend renders whatever a descriptor
// declares rather than a hard-coded Security Review form (§38.27). Fixture only — not a real schema.
func releaseDescriptor() core.FormDescriptor {
	return core.FormDescriptor{
		FormRef:     ReleaseFormRef,
		Title:       "Release Readiness",
		Description: "Demo configuration for the release readiness workflow (fixture only).",
		Fields: []core.FormField{
			{Key: "version", Label: "Release version", Type: "text", Required: true, Placeholder: "v1.2.3"},
			{Key: "rolloutPercent", Label: "Rollout percent", Type: "number", Required: true, DefaultValue: 10},
			{
				Key: "environments", Label: "Target environments", Type: "multi_select", Required: true,
				Options: []core.FormOption{
					{Value: "staging", Label: "Staging"},
					{Value: "canary", Label: "Canary"},
					{Value: "production", Label: "Production"},
				},
			},
			{Key: "notifyOnCall", Label: "Notify on-call", Type: "boolean", Required: false, DefaultValue: true},
			{Key: "rollbackPlan", Label: "Rollback plan", Type: "textarea", Required: false, Placeholder: "How to roll back…"},
		},
	}
}

// MockInputAssistProvider produces deterministic AI Assist suggestions for the demo workflow: the same
// issue text and the same current values always yield the same patch. It performs no LLM call, uses no
// randomness, and only ever *suggests* — applying and confirming stay user actions (§38.12).
type MockInputAssistProvider struct{}

//nolint:gocritic // value-typed assist input mirrors the provider interface signature this mock satisfies
func (MockInputAssistProvider) Suggest(_ context.Context, in core.AssistInput) (core.AssistSuggestion, error) {
	values := core.Object{}
	explanations := core.Object{}
	text := strings.ToLower(in.IssueTitle + " " + in.IssueDescription)

	// Suggestions are keyed by well-known field names but only ever emitted for keys the *current*
	// descriptor actually declares, so one mock serves every fixture workflow. A key the user has
	// already filled in is never suggested over — assist must not clobber a typed value.
	declared := map[string]bool{}
	for i := range in.Descriptor.Fields {
		declared[in.Descriptor.Fields[i].Key] = true
	}
	suggest := func(key string, value any, reason string) {
		if !declared[key] || in.CurrentValues.S(key) != "" {
			return
		}
		values[key] = value
		explanations[key] = reason
	}

	security := strings.Contains(text, "security") || strings.Contains(text, "auth") || strings.Contains(text, "token")
	suggest("scope", "changed-files", "Demo suggestion: this issue describes a change, so reviewing the changed files is the narrowest useful scope.")
	if security {
		suggest("severity", "high", "Demo suggestion: the issue text mentions security/auth, so raise the severity.")
	} else {
		suggest("severity", "medium", "Demo suggestion: default demo severity for a non-security issue.")
	}
	suggest("branch", "main", "Demo suggestion: review the default branch when none is given.")
	suggest("version", "v1.2.3", "Demo suggestion: demo release version.")
	suggest("rolloutPercent", 10, "Demo suggestion: start with a small rollout percentage.")
	suggest("environments", []any{"staging"}, "Demo suggestion: validate in staging before production.")
	return core.AssistSuggestion{Values: values, Explanations: explanations}, nil
}

func (FixtureCollaborationDirectory) ListTargets(_ context.Context, _, query string) ([]core.CollaborationTargetSummary, error) {
	if query == "" {
		out := make([]core.CollaborationTargetSummary, len(fixtureTargets))
		copy(out, fixtureTargets)
		return out, nil
	}
	q := strings.ToLower(query)
	out := []core.CollaborationTargetSummary{}
	for _, t := range fixtureTargets {
		if strings.Contains(strings.ToLower(t.DisplayName), q) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (FixtureCollaborationDirectory) ResolveTarget(_ context.Context, _, targetType, targetID string) (core.CollaborationTargetSummary, bool, error) {
	for _, t := range fixtureTargets {
		if t.Type == targetType && t.ID == targetID {
			return t, true, nil
		}
	}
	return core.CollaborationTargetSummary{}, false, nil
}

// DeterministicContextBuilder assembles a fixed, reproducible execution context snapshot. The
// bounded recent-comments window and the explicit context refs are supplied by the Store; this
// builder only lays them out — it performs no AI.
type DeterministicContextBuilder struct{}

//nolint:gocritic // value-typed context input mirrors the ContextBuilder port; the fixture only lays the snapshot out
func (DeterministicContextBuilder) Build(_ context.Context, in core.ContextInput) (core.Object, error) {
	values := in.InteractionValues
	if values == nil {
		values = core.Object{}
	}
	return core.Object{
		"task":              in.Task,
		"interactionValues": values,
		"target":            core.Object{"type": in.TargetType, "id": in.TargetID},
		"issue":             core.Object{"title": in.IssueTitle, "description": in.IssueDescription},
		"recentComments":    in.RecentComments,
		"contextRefs":       in.ContextRefs,
	}, nil
}

// MockExecutionDispatcher reports an accepted handoff with a synthetic external id and completes
// synchronously by driving the observer lifecycle. All mock behavior (including the fixed reply text)
// lives here, not in the Issue core — and nothing here performs a real scan, review or LLM call.
type MockExecutionDispatcher struct{}

//nolint:gocritic // value-typed dispatch request mirrors the ExecutionDispatcher port; the mock only records the handoff
func (MockExecutionDispatcher) Dispatch(_ context.Context, req core.DispatchRequest) (core.DispatchResult, error) {
	return core.DispatchResult{Accepted: true, ExternalExecutionID: "mock-" + req.RunID}, nil
}

// Execute drives one run to completion. Agent/team runs reply as a comment (queued -> dispatched ->
// running -> reply -> completed); a workflow run reports coarse progress and a human-readable result,
// which the Store projects as `system` activities rather than comments (§38.26).
//nolint:gocritic // value-typed dispatch request mirrors the ExecutionDispatcher port; the mock drives the observer synchronously
func (MockExecutionDispatcher) Execute(ctx context.Context, req core.DispatchRequest, obs core.ExecutionObserver) {
	_, _ = obs.ObserveStarted(ctx, req.TenantID, req.RunID)
	if req.ExecutorType == "workflow" {
		_, _ = obs.ObserveProgress(ctx, req.TenantID, req.RunID, "Reading repository metadata")
		_, _ = obs.ObserveMessage(ctx, req.TenantID, req.RunID, mockWorkflowResult(req.Input))
		_, _ = obs.ObserveCompleted(ctx, req.TenantID, req.RunID, core.Object{"mock": true, "executorType": "workflow", "executorId": req.ExecutorID})
		return
	}
	_, _ = obs.ObserveMessage(ctx, req.TenantID, req.RunID, mockReply(req.ExecutorType))
	_, _ = obs.ObserveCompleted(ctx, req.TenantID, req.RunID, core.Object{"mock": true, "executorType": req.ExecutorType, "executorId": req.ExecutorID})
}

func mockReply(executorType string) string {
	if executorType == "team" {
		return "Demo response: The team received this task and context."
	}
	return "Demo response: I received the task and the supplied Issue context."
}

// mockWorkflowResult is a deterministic, explicitly-simulated summary of the confirmed form values. It
// is descriptor-agnostic (it echoes whatever was confirmed) and must never claim that real work ran.
func mockWorkflowResult(input core.Object) string {
	values := input.O("interactionValues")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+renderValue(values[key]))
	}
	if len(parts) == 0 {
		parts = append(parts, "no values")
	}
	return "Demo workflow result (simulated; no real work was performed): " + strings.Join(parts, ", ") + "."
}

func renderValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, renderValue(item))
		}
		return "[" + strings.Join(items, ", ") + "]"
	default:
		return fmt.Sprint(typed)
	}
}

// WireDevelopmentFixtures installs the in-memory dev/demo collaboration ports on the Store. It is
// the single composition point for the fixture set; callers must gate it behind an explicit,
// development-only configuration (cmd/server via `collaboration.development_fixtures` /
// CLOUD_COLLABORATION_DEVELOPMENT_FIXTURES; cmd/ora-web and the integration suite are dev edges and
// opt in unconditionally). Production default is OFF — with the ports left nil, only human targets
// are served and no fake Agent/Team/Workflow is exposed. It must never be enabled as a side effect
// of any external login provider being configured.
func WireDevelopmentFixtures(store *core.Store) {
	store.Directory = FixtureCollaborationDirectory{}
	store.Context = DeterministicContextBuilder{}
	store.Dispatcher = MockExecutionDispatcher{}
	store.Forms = FixtureFormDescriptorProvider{}
	store.Assist = MockInputAssistProvider{}
}
