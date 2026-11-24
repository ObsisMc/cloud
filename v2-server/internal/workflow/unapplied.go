package workflow

// placement is the graph context of a node: the Iteration region or the Loop body that holds it.
// It is derived from the authored node alone, so a spare member has the placement it would have
// once wired. Containers do not nest, so at most one field is set.
type placement struct {
	// region is the Iteration whose region holds the node, or empty.
	region string
	// loop is the Loop whose body holds the node, or empty.
	loop string
}

func placementOf(n *node) placement {
	if n.container {
		return placement{loop: n.owner}
	}
	return placement{region: n.owner}
}

// unappliedRule has the signature of unappliedAgentSettings. The analysis takes the rule as a
// parameter only so tests can prove it feeds the rule the same inputs as the run path.
type unappliedRule func(config object, at placement) []string

// unappliedAgentSettings is the single decision on which configured Agent settings a run saves
// but does not apply. Node.Agent records its result in the attempt payload and AnalyzeWorkflow
// reports it to the editor; both pass the node's placement, so a rule that depends on where the
// node sits reads the same inputs on both paths. The order is the payload's order.
func unappliedAgentSettings(config object, at placement) []string {
	var unapplied []string
	interactive := agentInteractive(config)
	// v2 parks a root-graph interactive Agent for its starter; a Loop member runs automatically
	// (desktop would park it). Publication rejects interactive Iteration members, so they never run.
	if interactive && at.loop != "" {
		unapplied = append(unapplied, "interactive")
	}
	// Retry is unapplied where desktop would retry and v2 does not. Desktop never retries an
	// interactive node, which a person drives (node_runtime.rs:373-379 at ebd5b725), so retry is
	// not listed there; elsewhere retryApplies alone says where v2 retries.
	if retryWouldRun(config) && !interactive && !retryApplies(config, at) {
		unapplied = append(unapplied, "retry")
	}
	return unapplied
}

// retryApplies is the single answer to "will v2 automatically retry this Agent node": its
// policy would retry and it is not interactive. Root, Loop-body and Iteration-region Agents all
// apply; desktop never retries an interactive node (node_runtime.rs:373-379 at ebd5b725).
// unappliedAgentSettings and the engine's retry decision both call it with the node's placement,
// so the editor, the run payload and the runtime agree.
func retryApplies(config object, at placement) bool {
	return retryWouldRun(config) && !agentInteractive(config)
}

// agentInteractive reports whether an Agent is configured for a conversation a person drives.
func agentInteractive(config object) bool {
	interactive, _ := config["interactive"].(bool)
	return interactive
}

// enabledBindings lists the IDs of the Skill or MCP bindings under key that a run receives: every
// item not explicitly disabled, so a binding without an enabled flag counts as on. It is the one
// reading of "enabled" for both AgentConfig and the unapplied decision.
func enabledBindings(config object, key, idField string) []string {
	var ids []string
	items, _ := config[key].([]any)
	for _, item := range items {
		if v, _ := item.(object); v["enabled"] != false {
			id, _ := v[idField].(string)
			ids = append(ids, id)
		}
	}
	return ids
}

// retryWouldRun reports whether desktop's effective retry policy would rerun a failed attempt
// (desktop crates/application/src/workflow_run/engine/retry.rs:66-74, 96-99 at ebd5b725): a missing
// or null policy is the default, enabled with retries left; an explicit one retries only when
// enabled with a positive budget. It reads the policy only; where retry applies is the caller's.
func retryWouldRun(config object) bool {
	p := retryPolicyOf(config)
	return p.enabled && p.max > 0
}

// unappliedSettings lists every Agent node of the document, spare ones included because the
// inspector explains a setting wherever it is edited, with the settings rule returns for it.
func unappliedSettings(doc *document, rule unappliedRule) []UnappliedSetting {
	result := []UnappliedSetting{}
	for _, n := range doc.nodes {
		if n.kind != "agent" {
			continue
		}
		config, _ := n.data["agentConfig"].(object)
		if settings := rule(config, placementOf(n)); len(settings) != 0 {
			result = append(result, UnappliedSetting{NodeID: n.id, Settings: settings})
		}
	}
	return result
}
