//go:build linux

// Package agentruntime adapts the fixed local Node to already-admitted execution sessions.
// Admission, permissions, public event sequence allocation and SQL belong to its caller.
package agentruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
)

// Project contains trusted repository preparation outputs, never paths supplied by an Agent.
type Project struct {
	MirrorPath  string
	Commit      string
	SnapshotRef string
}

// UserSkill is one validated skill document materialized into a new checkout.
type UserSkill struct {
	Name        string
	Description string
	Content     string
}

// Options wires preverified local assets and frozen model bindings into the adapter.
// ResolveProject must check Scope access and return a prepared mirror and immutable snapshot.
type Options struct {
	Root            string
	AssetsDirectory string
	GitBackend      string
	Bindings        map[string]nodeservices.Binding
	ObserveProvider func(execution.Ref, nodeservices.Binding) nodeservices.Provider
	ResolveProject  func(context.Context, execution.Scope, string) (Project, error)
	ReadMCPTarget   func(context.Context, execution.Scope, string, *execution.MCPSelection, int64) (*execution.MCPTarget, error)
	// UserSkills returns the caller's live skill catalog. Nil skips user-skill projection.
	UserSkills func(context.Context, execution.Scope) ([]UserSkill, error)
	// RecordWorkspace remembers where a workspace's checkout is, once it is ready and before the
	// Agent starts, so pages can read its files later. Nil records nothing.
	RecordWorkspace func(ctx context.Context, workspaceID string, checkout Checkout) error
	// ForgetCheckout drops the record of a reattached checkout whose clone execution Node no
	// longer knows, so a later process does not reattach it again. Nil forgets nothing.
	ForgetCheckout func(ctx context.Context, workspaceID, cloneExecution string) error
}

// Checkout is a workspace's directory on this host and the commit it was cloned at.
// CloneExecution is the Node clone execution behind it: Node resolves a session's checkout from
// that ID in its own ledger, also after Node restarted, so a recovered session can start on it.
type Checkout struct {
	Path           string
	BaseCommit     string
	CloneExecution string
}

// Emit retains an event before its source Node envelope can be acknowledged. It must honor ctx.
// TurnEnded must durably retain the first terminal result before returning nil. The caller may
// stage this result privately until it can publish the terminal event with its delivery.
type Emit func(context.Context, execution.Event) error

// Driver owns one Node per project and one physical Agent execution per logical session.
type Driver struct {
	opts     Options
	boot     string
	mu       sync.Mutex
	closed   bool
	projects map[string]*projectRuntime
	sessions map[string]*agentSession
	// recorded are workflow checkouts an earlier Node incarnation prepared, by workspace (see
	// Reattach): Node resolves them from their clone execution in its own ledger, so a later
	// session of the workspace starts on the same directory instead of a fresh clone.
	recorded map[string]Checkout
	// projectLocks stand in for a project's skillsMu while it has no runtime (see projectLock).
	projectLocks   map[string]*sync.Mutex
	serial         atomic.Uint64
	controlTimeout time.Duration
	mcpRefreshWait time.Duration
	// turnCancelWait and turnCancelLimit bound the wait for TurnCancelSettled (see
	// defaultTurnCancelWait).
	turnCancelWait  time.Duration
	turnCancelLimit time.Duration
	startProject    func(*projectRuntime) error
	// wrapClient lets real-Node tests interpose on IPC before the receiver starts.
	wrapClient func(nodeClient) nodeClient
}

// New validates deployment wiring. Processes start lazily for an already-admitted project turn.
func New(opts Options) (*Driver, error) {
	if !filepath.IsAbs(opts.Root) || !filepath.IsAbs(opts.AssetsDirectory) || !filepath.IsAbs(opts.GitBackend) || opts.ResolveProject == nil || len(opts.Bindings) == 0 {
		return nil, errors.New("invalid Agent runtime configuration")
	}
	for _, plugin := range agentPlugins {
		if _, err := plugin.pkg(); err != nil {
			return nil, err
		}
	}
	// A project's runtime root is <Root>/<16 hex digits>/w; Node's control socket is its longest path.
	if filepath.Clean(opts.Root) != opts.Root || len(node.ControlSocket(filepath.Join(opts.Root, "0000000000000000", "w"))) >= 108 {
		return nil, errors.New("Agent runtime root exceeds the Unix socket path limit; choose a shorter, clean V2_DATA_DIR")
	}
	bindings := make(map[string]nodeservices.Binding, len(opts.Bindings))
	for id, binding := range opts.Bindings {
		if id == "" || binding.Provider == nil || binding.Model.ID == "" {
			return nil, errors.New("invalid Agent runtime binding")
		}
		bindings[id] = binding
	}
	opts.Bindings = bindings
	if err := os.MkdirAll(opts.Root, 0700); err != nil {
		return nil, execution.ErrUnavailable
	}
	recovery, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := Recover(recovery, opts.Root); err != nil {
		return nil, err
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, execution.ErrUnavailable
	}
	return &Driver{opts: opts, boot: hex.EncodeToString(nonce[:]), projects: map[string]*projectRuntime{}, sessions: map[string]*agentSession{}, recorded: map[string]Checkout{}, controlTimeout: 2 * time.Minute, mcpRefreshWait: 5 * time.Minute, turnCancelWait: defaultTurnCancelWait, turnCancelLimit: execution.TurnStopSettleLimit}, nil
}

// Reattach makes a workflow workspace's checkout, prepared by an earlier process, the one its
// sessions start on: Node resolves it from its clone execution in its own ledger, also on a new
// Node incarnation, so its files and uncommitted edits stay and nothing is cloned again. Callers
// reattach before any session of the workspace starts; a checkout this process already prepared
// for the workspace wins.
func (d *Driver) Reattach(workspaceID, cloneExecution, path string) {
	if workspaceID == "" || cloneExecution == "" || path == "" {
		return
	}
	d.mu.Lock()
	d.recorded[workspaceID] = Checkout{Path: path, CloneExecution: cloneExecution}
	d.mu.Unlock()
}

// Run executes the admitted turn. Normal Append reuses its original live Agent conversation. A
// turn that fails before the session's Agent was ever sent its start is marked NotStarted. A
// failed turn whose command never left for Node, or that Node refused without running it, is
// marked NotDelivered: its prompt never reached the Agent, whatever became of the session.
func (d *Driver) Run(ctx context.Context, scope execution.Scope, session execution.Session, turn execution.Turn, emit func(context.Context, execution.Event) error) (execution.Result, error) {
	result, err := d.run(ctx, scope, session, turn, emit)
	if err != nil && result.Status == execution.StatusFailed && !d.agentStarted(session.ID) {
		result.NotStarted = true
	}
	if result.Status == execution.StatusFailed && !d.turnSent(session.ID, turn.Ref.TurnID) {
		result.NotDelivered = true
	}
	return result, err
}

// turnSent reports whether the turn's command may have reached Node and Node has not refused it.
// The execution service runs one turn of a session at a time, so the session's record is this
// turn's until Run returns; a session whose preparation failed is not retained and sent nothing.
func (d *Driver) turnSent(sid, turn string) bool {
	d.mu.Lock()
	a := d.sessions[sid]
	d.mu.Unlock()
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sent == turn
}

// agentStarted reports whether the session's Agent was sent its start. A session whose
// preparation failed is not retained, so it reads as not started.
func (d *Driver) agentStarted(sid string) bool {
	d.mu.Lock()
	a := d.sessions[sid]
	d.mu.Unlock()
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

func (d *Driver) run(ctx context.Context, scope execution.Scope, session execution.Session, turn execution.Turn, emit func(context.Context, execution.Event) error) (execution.Result, error) {
	if session.ID == "" || session.WorkspaceID == "" || session.Target == nil || session.Target.ProjectID == "" || session.Executor == nil || turn.Ref.SessionID != session.ID || turn.Ref.TurnID == "" || emit == nil {
		return failed("agent_request_invalid"), execution.ErrInvalidRequest
	}
	if session.EndedAt != nil {
		return failed("agent_session_ended"), execution.ErrSessionEnded
	}
	// History handoff starts a different provider session. Once this lifecycle's
	// Node is physically gone, it cannot establish delivery on the original one.
	if session.Recovery != nil && (turn.MCPGeneration != nil || session.MCP != nil && len(session.MCP.Bindings) != 0) {
		return failed(execution.FailureNodeLost), execution.ErrNodeLost
	}
	text, err := turn.Prompt.Text()
	limit := 64 * 1024
	if turn.Host != nil {
		limit -= execution.MaxHostInstruction
	}
	if err != nil || len(text) > limit {
		return failed("agent_request_invalid"), execution.ErrInvalidRequest
	}
	plugin, ok := findPlugin(session.Executor.AgentID, session.Executor.PluginVersion)
	if !ok {
		return failed("agent_executor_unsupported"), execution.ErrUnsupported
	}
	binding, ok := d.opts.Bindings[session.Executor.Model.ID]
	if !ok {
		return failed("agent_model_unavailable"), execution.ErrUnavailable
	}
	if binding.Protocol != plugin.protocol {
		return failed("agent_executor_unsupported"), execution.ErrUnsupported
	}
	project, err := d.opts.ResolveProject(ctx, scope, session.Target.ProjectID)
	if err != nil {
		return failed("agent_project_unavailable"), execution.ErrUnavailable
	}
	timing := newPreparation(turn.Ref, emit)
	// A session that is already prepared never starts a stage, so later turns publish nothing.
	defer func() { timing.finish(time.Now()) }()
	a, err := d.session(ctx, scope, session, project, turn, timing)
	if err != nil {
		var mcp *execution.MCPFailure
		if errors.As(err, &mcp) {
			result := failed(mcp.Code)
			result.MCPFailure = mcp
			return result, err
		}
		if ctx.Err() != nil {
			return d.cancelledRun(session.ID, ctx.Err())
		}
		if workflowSkillError(err) {
			return failed(err.Error()), err
		}
		if errors.Is(err, execution.ErrHostMCPUnavailable) {
			return failed(execution.FailureHostMCPNode), err
		}
		return failed("agent_prepare_failed"), err
	}
	a.commandMu.Lock()
	call, fresh, err := a.begin(turn.Ref.TurnID, session.MCP, emit)
	if err != nil {
		a.commandMu.Unlock()
		var failure *execution.MCPFailure
		if errors.As(err, &failure) {
			a.requestEnd()
			result := failed(failure.Code)
			result.MCPFailure, result.AgentLost = failure, true
			return result, failure
		}
		if errors.Is(err, execution.ErrNodeLost) {
			return failed(execution.FailureNodeLost), err
		}
		return failed("agent_session_unavailable"), err
	}
	if fresh {
		timing.enter(stageAgent, time.Now())
		a.mu.Lock()
		a.timing = timing
		a.mu.Unlock()
	}
	err = a.submit(turn, text, fresh)
	a.commandMu.Unlock()
	if err != nil {
		code := "agent_dispatch_failed"
		select {
		case <-a.project.dead:
			// The Node died while the turn was being sent. Part of it may have arrived and run,
			// so the turn counts as lost with its Node, not as one that never reached the Agent.
			code = execution.FailureNodeLost
		default:
		}
		a.fail(code, err)
		a.requestEnd()
	}
	var refreshDeadline time.Time
	// A stop the caller asked for (StopTurn) also interrupts a wait for an MCP refresh. The
	// goroutine ends with Run.
	refreshCtx, stopRefresh := context.WithCancel(ctx)
	defer stopRefresh()
	go func() {
		select {
		case <-call.stopRequested:
			stopRefresh()
		case <-refreshCtx.Done():
		}
	}()
	// ended ends the session: for End, Close and shutdown (ctx), and for a stop the session cannot
	// keep, as every stop did before turn_cancel. Whatever the turn's outcome, the session is gone
	// from here on, so the outcome reports it lost, even a late stopped settlement (fallingBack).
	ended := func() (execution.Result, error) {
		a.mu.Lock()
		call.fallingBack = true
		a.mu.Unlock()
		reason := ctx.Err()
		if reason == nil {
			reason = execution.ErrSessionEnded
		}
		result, err := d.cancelledRun(session.ID, reason)
		select {
		case outcome := <-call.done:
			if outcome.err == nil {
				outcome.result.SessionKept, outcome.result.AgentLost, outcome.err = false, true, execution.ErrSessionEnded
			}
			return outcome.result, outcome.err
		default:
			return result, err
		}
	}
	stopRequested := call.stopRequested
	// While Node settles a stop (cancel-turn ADR D4), window ends the wait unless an event of this
	// session arrives first, which restarts it; limit bounds the whole wait.
	var window *time.Timer
	var windowEnded, limitEnded <-chan time.Time
	for {
		select {
		case outcome := <-call.done:
			if turn.Caller.Kind == execution.CallerChat && outcome.err == nil {
				a.project.mu.Lock()
				a.project.fillSpareLocked(project)
				a.project.mu.Unlock()
			}
			return outcome.result, outcome.err
		case <-call.updateRequired:
			if refreshDeadline.IsZero() {
				refreshDeadline = time.Now().Add(d.mcpRefreshWait)
			}
			err := a.reconcileTurn(refreshCtx, refreshDeadline, call, turn, text)
			// Service.Cancel owns the physical stop and its confirmation; a requested stop is
			// handled below.
			if err == nil || refreshCtx.Err() != nil {
				continue
			}
			if errors.Is(err, errMCPRefreshWait) {
				// Node refused the command for its MCP generation and never queued it, so the
				// prompt never reached the Agent; the session stays for the next turn.
				result := failed(errMCPRefreshWait.Error())
				result.NotDelivered = true
				if a.publish(execution.Event{Ref: turn.Ref, Kind: execution.EventTurnEnded, Result: &result}) == nil {
					a.complete(turnOutcome{call.id, result, nil})
					continue
				}
				a.fail("agent_event_write_failed", errEventStore)
			} else {
				a.fail("mcp_update_failed", err)
			}
			a.requestEnd()
		case <-stopRequested:
			stopRequested = nil
			switch a.stopTurn(call) {
			case turnStopSent:
				// Only the turn stops; its settlement arrives on call.done.
				window = time.NewTimer(d.turnCancelWait)
				defer window.Stop()
				limit := time.NewTimer(d.turnCancelLimit)
				defer limit.Stop()
				windowEnded, limitEnded = window.C, limit.C
				continue
			case turnStopFinished:
				outcome := <-call.done
				return outcome.result, outcome.err
			}
			return ended()
		case <-call.progress:
			if window != nil {
				window.Reset(d.turnCancelWait)
			}
		case <-ctx.Done():
			// End, Close or shutdown: the session ends now, also while a stop awaits settlement.
			return ended()
		case <-call.cancelFallback:
			return ended()
		case <-windowEnded:
			return ended()
		case <-limitEnded:
			return ended()
		}
	}
}

// StopTurn asks to stop only the turn ref of a session that chose keep_session_on_cancel, the way
// execution.Service.Cancel wants a stop. It reports false when that session cannot keep its Agent
// through a stop (it did not choose to, its Agent has not been started, it is ending or broken);
// the caller then ends the session instead. A turn that has not begun yet is stopped when it does;
// one that already ended keeps its outcome.
func (d *Driver) StopTurn(ref execution.Ref) bool {
	d.mu.Lock()
	a := d.sessions[ref.SessionID]
	d.mu.Unlock()
	if a == nil {
		return false
	}
	select {
	case <-a.ready:
	default:
		return false // still preparing: nothing was started that a stop could keep
	}
	if a.prepareErr != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started || !a.keep.onCancel || a.ended || a.ending || a.broken {
		return false
	}
	if a.active != nil && a.active.id == ref.TurnID {
		a.active.requestStop()
		return true
	}
	a.stopNext = ref.TurnID
	return true
}

// End waits for the physical session to stop; it does not release the caller's SQL directory lock.
func (d *Driver) End(ctx context.Context, sid string) error {
	d.mu.Lock()
	a := d.sessions[sid]
	d.mu.Unlock()
	if a == nil {
		return nil
	}
	select {
	case <-a.ready:
	case <-ctx.Done():
		return execution.ErrStopUnconfirmed
	}
	select {
	case <-a.stopped:
		return nil
	default:
	}
	if a.prepareErr != nil && a.project != nil && a.project.initErr != nil {
		if err := a.project.close(ctx); err != nil {
			return err
		}
		a.markStopped()
		return nil
	}
	a.requestEnd()
	if a.project == nil {
		return nil
	}
	select {
	case <-a.stopped:
		return nil
	default:
	}
	a.project.mu.Lock()
	fatal := a.project.fatal
	a.project.mu.Unlock()
	if fatal {
		// An incarnation marked for rebuild is no longer trusted to acknowledge End.
		return d.retire(ctx, a.project)
	}
	select {
	case <-a.stopped:
		return nil
	case <-ctx.Done():
		return execution.ErrStopUnconfirmed
	case <-a.project.dead:
		// A disconnected Node cannot acknowledge End. Retire its entire incarnation
		// and require the supervisor's process-tree proof before releasing any owner.
		return d.retire(ctx, a.project)
	}
}

// retire stops a project's whole incarnation through its supervisor and forgets it only after
// that proof, so the next session builds a fresh Node and never overlaps the old process tree.
func (d *Driver) retire(ctx context.Context, p *projectRuntime) error {
	if err := p.close(ctx); err != nil {
		return execution.ErrStopUnconfirmed
	}
	d.dropProject(p)
	return nil
}

// Rebuild marks the Node serving sid for replacement after its stop could not be confirmed.
// It stops nothing itself: the project's next session, or End, first retires the old process
// tree through the supervisor and only then starts a new Node. Other projects are unaffected.
func (d *Driver) Rebuild(sid string) {
	d.mu.Lock()
	a := d.sessions[sid]
	d.mu.Unlock()
	if a == nil {
		return
	}
	select {
	case <-a.ready:
	default:
		// Preparation still owns a.project; its own bounded wait settles that session.
		return
	}
	if a.project == nil {
		return
	}
	a.project.mu.Lock()
	a.project.fatal = true
	a.project.mu.Unlock()
}

// Close stops all owned sessions and Nodes. A failed stop remains retryable.
func (d *Driver) Close(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	projects := make([]*projectRuntime, 0, len(d.projects))
	for _, p := range d.projects {
		projects = append(projects, p)
	}
	d.mu.Unlock()
	var result error
	for _, p := range projects {
		if err := p.close(ctx); err != nil {
			result = errors.Join(result, execution.ErrStopUnconfirmed)
		}
	}
	if result == nil {
		d.mu.Lock()
		sessions := make([]*agentSession, 0, len(d.sessions))
		for _, session := range d.sessions {
			sessions = append(sessions, session)
		}
		d.mu.Unlock()
		for _, session := range sessions {
			select {
			case <-session.ready:
				session.markStopped()
			case <-ctx.Done():
				return execution.ErrStopUnconfirmed
			}
		}
	}
	return result
}

func (d *Driver) cancelledRun(sid string, reason error) (execution.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.End(ctx, sid); err != nil {
		return failed("agent_stop_unconfirmed"), err
	}
	return execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled"}, reason
}

func failed(code string) execution.Result {
	return execution.Result{Status: execution.StatusFailed, FailureCode: code}
}
