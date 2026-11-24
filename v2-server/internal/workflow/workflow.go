// Package workflow owns v2 workflow document analysis and publication validation.
// It has no persistence, transport, catalog, or execution dependencies.
package workflow

import (
	"fmt"
)

// Analysis separates known spare nodes from unrecognized authoring node kinds, and reports what
// a run would not honor: Agent settings it saves but runs without, and reachable nodes whose kind
// start refuses. Every slice is non-nil, including for an empty document.
type Analysis struct {
	UnusedNodeIDs     []string           `json:"unusedNodeIds"`
	UnrecognizedNodes []UnrecognizedNode `json:"unrecognizedNodes"`
	UnappliedSettings []UnappliedSetting `json:"unappliedSettings"`
	UnsupportedNodes  []UnsupportedNode  `json:"unsupportedNodes"`
}

// UnappliedSetting lists, for one Agent node, the configured settings a run records as unapplied.
// Settings comes from unappliedAgentSettings with the node's placement, the same call that fills
// AgentConfig.Unapplied for the attempt payload.
type UnappliedSetting struct {
	NodeID   string   `json:"nodeId"`
	Settings []string `json:"settings"`
}

// UnsupportedNode is a reachable node of a known kind that run start refuses with
// workflow_run_unsupported_node_type, as desktop's NodeType::supported does.
type UnsupportedNode struct {
	NodeID string `json:"nodeId"`
	Kind   string `json:"kind"`
}

// UnrecognizedNode describes a kind the desktop graph vocabulary does not register.
type UnrecognizedNode struct {
	NodeID string `json:"nodeId"`
	Kind   string `json:"kind"`
}

// ValidationError identifies a document defect without exposing host or database details.
// Code is a domain category; the HTTP adapter owns its public error-code mapping.
type ValidationError struct {
	Code   string
	NodeID string
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	where := e.Field
	if e.NodeID != "" {
		where = "node " + e.NodeID + ": " + where
	}
	return fmt.Sprintf("workflow %s (%s): %s", e.Code, where, e.Reason)
}

// FaultParams names the refused node, field and reason for the public
// workflow_run_graph_parse fault, so an author can find what to fix. The reason is a bounded
// diagnostic about the submitted document; it carries no host or database detail.
func (e *ValidationError) FaultParams() map[string]any {
	params := map[string]any{"reason": e.Reason}
	if e.NodeID != "" {
		params["nodeId"] = e.NodeID
	}
	if e.Field != "" {
		params["field"] = e.Field
	}
	return params
}

func invalid(code, nodeID, field, reason string) error {
	return &ValidationError{Code: code, NodeID: nodeID, Field: field, Reason: reason}
}

// AnalyzeWorkflow validates authoring identities and containment, then reports
// entry-unreachable nodes, unapplied Agent settings and reachable kinds start refuses.
// It does not require complete executable configuration.
// The supplied graph is never rewritten or persisted.
func AnalyzeWorkflow(graph string) (Analysis, error) {
	doc, err := project(graph)
	if err != nil {
		return Analysis{}, err
	}
	analysis := doc.analysis
	analysis.UnappliedSettings = unappliedSettings(doc, unappliedAgentSettings)
	analysis.UnsupportedNodes = []UnsupportedNode{}
	for _, n := range doc.nodes {
		// Without a root Start nothing is reachable; start then fails for the missing entry first.
		if _, runs := runtimes[n.kind]; doc.rooted && doc.active[n.id] && knownKind(n.kind) && !runs {
			analysis.UnsupportedNodes = append(analysis.UnsupportedNodes, UnsupportedNode{NodeID: n.id, Kind: n.kind})
		}
	}
	return analysis, nil
}

// ValidateWorkflow applies desktop's publish-time graph parser to the reachable
// projection, including node configuration, variable declarations and container
// boundaries. It does not assert start-time readiness or installed capabilities.
// Drafts should be saved without invoking this function.
func ValidateWorkflow(graph string) error {
	doc, err := project(graph)
	if err != nil {
		return err
	}
	return validateDocument(doc)
}

// ValidateImportedWorkflow validates a workflow document imported from a plugin package.
// It enforces the same topology, declaration, container and node configuration rules as
// ValidateWorkflow, but allows active Agent nodes without a selected executor so marketplace
// templates can be imported and configured before running or re-publishing.
func ValidateImportedWorkflow(graph string) error {
	doc, err := project(graph)
	if err != nil {
		return err
	}
	_, err = buildScopes(doc)
	return err
}
