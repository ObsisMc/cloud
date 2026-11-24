//go:build linux

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
)

type mcpRevision struct {
	Generation *int64 `json:"generation"`
	Members    []struct {
		ID       string `json:"plugin_id"`
		State    string `json:"state"`
		Version  string `json:"plugin_version"`
		Revision int64  `json:"configuration_revision"`
		Code     string `json:"code"`
	} `json:"members"`
}

type mcpReply struct {
	Node    node.Identity `json:"node"`
	Request string        `json:"request_id"`
	Result  string        `json:"result"`
	State   *mcpSnapshot  `json:"state"`
}

type mcpSnapshot struct {
	Desired     mcpRevision  `json:"desired"`
	Active      *mcpRevision `json:"active"`
	Convergence string       `json:"convergence"`
	Capacity    *bool        `json:"probe_capacity_available"`
	Members     []struct {
		ID     string `json:"plugin_id"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		Attempt  *int64 `json:"probe_attempt"`
		Queued   bool   `json:"queued"`
		InFlight bool   `json:"probe_in_flight"`
		Cleanup  bool   `json:"cleanup_pending"`
	} `json:"members"`
}

// Read-only observations and update replies have no sequence, journal entry or EventAck.
func (a *agentSession) receiveMCPReply(message node.Message) {
	if message.Version != 1 || message.Sequence != nil || message.OperationID != "op-"+a.execution {
		return
	}
	var reply mcpReply
	if json.Unmarshal(message.Payload, &reply) != nil || reply.Node != a.project.ipc().Identity() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if waiting := a.mcpRequests[reply.Request]; waiting != nil {
		select {
		case waiting <- reply:
		default:
		}
	}
}

func (a *agentSession) mcpRequest(ctx context.Context, kind string, payload map[string]any) (mcpReply, error) {
	id := a.project.driver.nextID("m")
	wait := make(chan mcpReply, 1)
	a.mu.Lock()
	if a.ending || a.ended {
		a.mu.Unlock()
		return mcpReply{}, execution.ErrSessionEnded
	}
	if a.mcpRequests == nil {
		a.mcpRequests = make(map[string]chan mcpReply)
	}
	if len(a.mcpRequests) >= 64 {
		a.mu.Unlock()
		return mcpReply{}, execution.ErrUnavailable
	}
	a.mcpRequests[id] = wait
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.mcpRequests, id); a.mu.Unlock() }()
	payload["request_id"], payload["node_id"] = id, a.project.nodeID()
	if err := ctx.Err(); err != nil {
		return mcpReply{}, err
	}
	if err := a.project.sendContext(ctx, kind, a.execution, payload); err != nil {
		return mcpReply{}, err
	}
	select {
	case reply := <-wait:
		return reply, nil
	case <-a.stopped:
		return mcpReply{}, execution.ErrSessionEnded
	case <-a.project.dead:
		return mcpReply{}, execution.ErrNodeLost
	case <-ctx.Done():
		return mcpReply{}, ctx.Err()
	}
}

func (r *mcpRevision) matchesSelection(s *execution.MCPSelection) bool {
	if r.Generation == nil || *r.Generation < 0 || *r.Generation > execution.MaxMCPCounter || len(r.Members) != len(s.Bindings) {
		return false
	}
	for i, member := range r.Members {
		if member.ID != s.Bindings[i].PluginID {
			return false
		}
	}
	return true
}

// syncMCP checks the committed generation before querying Node and sending an update.
// A lost reply is reconciled by observation, never by relabelling new values as an old update.
func (a *agentSession) syncMCP(ctx context.Context, selected *execution.MCPSelection, floor int64) error {
	select {
	case a.mcpSync <- struct{}{}:
		defer func() { <-a.mcpSync }()
	case <-ctx.Done():
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	target, err := a.project.driver.opts.ReadMCPTarget(ctx, a.scope, a.frozen.ID, selected, a.mcpDelivered)
	if err != nil {
		return err
	}
	if target == nil && floor == 0 {
		return nil
	}
	state, err := a.mcpRequest(ctx, "get_session_mcp_state", map[string]any{})
	if err != nil {
		return err
	}
	if state.Result != "accepted" || state.State == nil || !state.State.Desired.matchesSelection(selected) {
		return execution.ErrUnavailable
	}
	known := *state.State.Desired.Generation
	if target == nil {
		if known != a.mcpDelivered || known < floor {
			return execution.ErrConflict
		}
		return nil
	}
	if target.Generation < known || target.Generation < floor {
		return execution.ErrConflict
	}
	if target.Generation == known {
		if !state.State.Desired.matchesTarget(target) {
			return execution.ErrConflict
		}
		a.mcpDelivered = known
		return nil
	}
	members := make([]any, 0, len(selected.Bindings))
	index := 0
	for _, original := range selected.Bindings {
		id := original.PluginID
		if code, missing := target.Unavailable[id]; missing {
			members = append(members, map[string]string{"state": "unavailable", "plugin_id": id, "code": code})
			continue
		}
		if index >= len(target.Configured.Bindings) || index >= len(target.Configured.Packages) || target.Configured.Bindings[index].PluginID != id {
			return &execution.MCPFailure{Code: "mcp_selection_invalid"}
		}
		b := target.Configured.Bindings[index]
		pkg := target.Configured.Packages[index]
		// Settings-only updates use the already installed immutable version.
		if state.State.Desired.Members[len(members)].Version != b.PluginVersion {
			if err := a.project.installMCP(ctx, pkg); err != nil {
				return err
			}
		}
		index++
		members = append(members, map[string]any{"state": "configured", "plugin_id": id, "plugin_version": b.PluginVersion, "configuration_revision": b.Revision, "settings": b.Settings})
	}
	reply, err := a.mcpRequest(ctx, "update_session_mcp", map[string]any{"generation": target.Generation, "members": members})
	if err != nil {
		return err
	}
	if (reply.Result != "accepted" && reply.Result != "duplicate") || reply.State == nil || !reply.State.Desired.matchesSelection(selected) || !reply.State.Desired.matchesTarget(target) {
		return execution.ErrConflict
	}
	a.mcpDelivered = target.Generation
	return nil
}

func (r *mcpRevision) matchesTarget(t *execution.MCPTarget) bool {
	if r.Generation == nil || *r.Generation != t.Generation || len(r.Members) != len(t.Unavailable)+len(t.Configured.Bindings) {
		return false
	}
	i := 0
	for _, m := range r.Members {
		if code, ok := t.Unavailable[m.ID]; ok {
			if m.State != "unavailable" || m.Code != code {
				return false
			}
			continue
		}
		if i >= len(t.Configured.Bindings) {
			return false
		}
		b := t.Configured.Bindings[i]
		if m.State != "configured" || m.ID != b.PluginID || m.Version != b.PluginVersion || m.Revision != b.Revision {
			return false
		}
		i++
	}
	return i == len(t.Configured.Bindings)
}

var errMCPRefreshWait = errors.New("mcp_refresh_wait_timeout")

func (a *agentSession) reconcileTurn(ctx context.Context, deadline time.Time, call *activeTurn, turn execution.Turn, text string) error {
	ctx, cancel := context.WithDeadlineCause(ctx, deadline, errMCPRefreshWait)
	defer cancel()
	a.mu.Lock()
	selected := a.frozen.MCP
	a.mu.Unlock()
	if selected == nil || turn.MCPGeneration == nil || a.project.driver.opts.ReadMCPTarget == nil {
		return execution.ErrUnavailable
	}
	for {
		err := a.syncMCP(ctx, selected, *turn.MCPGeneration)
		if err == nil {
			break
		}
		if authorizationLost(err) {
			return err
		}
		// The command was explicitly refused. Wait for configuration repair or
		// capacity, without dispatching on the stale target or destroying the Agent.
		select {
		case <-a.project.dead:
			return execution.ErrNodeLost
		case <-a.stopped:
			return execution.ErrSessionEnded
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(time.Second):
		}
	}
	a.commandMu.Lock()
	defer a.commandMu.Unlock()
	a.mu.Lock()
	active := a.active == call && !a.ended && !a.ending && !a.broken
	a.mu.Unlock()
	if !active {
		return nil
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	// From here Node may hold the turn again, so a stop goes to Node (see stopTurn).
	a.mu.Lock()
	call.refused = false
	a.mu.Unlock()
	return a.submit(turn, text, false)
}

// MCPChanged wakes only sessions that selected this tenant's plugin. Multiple commits
// coalesce because the worker resolves the complete latest target from PostgreSQL.
func (d *Driver) MCPChanged(tenant, plugin string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, a := range d.sessions {
		if a.scope.TenantID != tenant {
			continue
		}
		a.mu.Lock()
		if a.mcpWake != nil && !a.ended && !a.ending && a.frozen.MCP != nil {
			for _, b := range a.frozen.MCP.Bindings {
				if b.PluginID == plugin {
					select {
					case a.mcpWake <- struct{}{}:
					default:
					}
					break
				}
			}
		}
		a.mu.Unlock()
	}
}

func (a *agentSession) followMCP(selected *execution.MCPSelection) {
	// Retry missed notifications and temporary transport or database failures.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.project.dead:
			return
		case <-a.stopped:
			return
		case <-a.mcpWake:
		case <-ticker.C:
		}
		if err := a.syncMCP(a.project.ctx, selected, 0); authorizationLost(err) {
			var failure *execution.MCPFailure
			if errors.As(err, &failure) {
				a.decideHost(failure.Code)
			}
			a.requestEnd()
			return
		}
	}
}

func authorizationLost(err error) bool {
	var failure *execution.MCPFailure
	return errors.Is(err, execution.ErrSessionEnded) || errors.Is(err, execution.ErrNotFound) || errors.As(err, &failure) && failure.Code == "mcp_selection_invalid"
}
