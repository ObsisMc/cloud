// Package completiontool is the host completion tool of interactive workflow Agent nodes: the
// per-attempt tokens that authorize it and the loopback MCP endpoint that serves it (desktop
// 22c5b83f crates/backend/src/workflow/run/interactive/agent_signal.rs and completion_server.rs;
// specs decisions/desktop/core/workflow/20261110-agent-completion-signal.md D1, D2 and
// decisions/node/agent-runtime/20261118-host-mcp-servers-in-agent-session-spec.md D6).
//
// A call only records that the Agent considers its step done. Whether the node completes stays
// with the completion gate of workflowhost, which binds the recorded signal to the turn the
// attempt settled. Everything here lives in memory, which rests on one premise: no Agent that an
// earlier process started can call the tool of this one. A killed v2-server may leave its Nodes
// running for a while, but startup stops every Node under its data directory, or refuses to start
// (agentruntime.Recover), before it recovers executions and before it serves this tool, so a restart
// always replaces Node and every credential (host MCP ADR D6). The next Agent of a parked node is
// started with new credentials.
package completiontool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

const (
	// ServerName is the host MCP server's name in an Agent's session setup.
	ServerName = "ora-workflow-completion"
	// ToolName is the single tool it serves.
	ToolName = "ora_complete_workflow_step"
)

// Signal is an accepted completion request of one attempt.
type Signal struct {
	ID string
	At time.Time
}

// Refusal is the stable reason the tool reports for a call it did not accept.
type Refusal string

const (
	// RefusedUnknown: the token was never issued, or belongs to another node.
	RefusedUnknown Refusal = "unknown attempt token"
	// RefusedStale: the attempt it was issued for is over.
	RefusedStale Refusal = "stale_attempt"
	// RefusedNotAwaiting: the node takes no completion request any more.
	RefusedNotAwaiting Refusal = "not_awaiting"
	// RefusedUnavailable: the node's stored state could not be read; the Agent may try again.
	RefusedUnavailable Refusal = "temporarily_unavailable"
)

// maxRevoked bounds the remembered reasons of withdrawn tokens, dropped wholesale when full.
const maxRevoked = 4096

// State is the stored state credentials and tokens depend on, whatever this process remembers.
type State interface {
	// HostExecutionRunning reports whether the execution is on record and not settled. A credential
	// counts only while the execution it was issued for is (host MCP ADR D6).
	HostExecutionRunning(ctx context.Context, executionID string) (bool, error)
	// WorkflowNodeTurnLive reports whether the node run executes a turn of the open session while
	// that execution of the session runs. The tool may be called only while it does.
	WorkflowNodeTurnLive(ctx context.Context, nodeRunID, sessionID, executionID string) (bool, error)
}

// Ending reports whether this process already decided why an execution of the session ended, which
// may be stored only later. It only refuses more than stored state does.
type Ending func(sessionID, executionID string) bool

// node is one interactive node run whose Agent may hold the tool.
type node struct {
	id string
	// credential names the endpoint of the node's current physical Agent, session the execution
	// session it belongs to, execution the host execution it runs in.
	credential, session, execution string
	// started says that Agent's start was sent to Node, connected that its endpoint was asked to
	// initialize or to list its tools.
	started, connected bool
	// attempts are the turns whose token may still be used, by attempt.
	attempts map[string]*Attempt
}

// Registry issues the credentials and tokens of every node of this process. A nil Registry is a
// deployment without the tool.
type Registry struct {
	state  State
	ending Ending
	now    func() time.Time

	mu          sync.Mutex
	url         string // the endpoint without its credential, set by Serve
	nodes       map[string]*node
	credentials map[string]*node
	tokens      map[string]*Attempt
	revoked     map[string]Refusal
}

// NewRegistry returns an empty registry that checks state, and what ending already knows.
func NewRegistry(state State, ending Ending) *Registry {
	return &Registry{state: state, ending: ending, now: time.Now, nodes: map[string]*node{}, credentials: map[string]*node{},
		tokens: map[string]*Attempt{}, revoked: map[string]Refusal{}}
}

// Attempt is the tool of one turn of a node run; the execution driver asks it for what the Agent
// gets. Its methods are safe after the attempt was revoked: what they then return never works.
type Attempt struct {
	r      *Registry
	node   *node
	id     string
	token  string
	signal *Signal
}

var _ execution.HostTools = (*Attempt)(nil)

// Attempt opens the turn attempt of nodeRunID. An earlier turn of the node must be over.
func (r *Registry) Attempt(nodeRunID, attempt string) *Attempt {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeRunID]
	if n == nil {
		n = &node{id: nodeRunID, attempts: map[string]*Attempt{}}
		r.nodes[nodeRunID] = n
	}
	a := &Attempt{r: r, node: n, id: attempt}
	n.attempts[attempt] = a
	return a
}

// Attach issues the endpoint credential of the node's new physical Agent, withdrawing the one its
// earlier Agent had.
func (a *Attempt) Attach(sessionID, executionID string) execution.HostServers {
	r := a.r
	r.mu.Lock()
	defer r.mu.Unlock()
	credential := secret()
	if a.open() {
		delete(r.credentials, a.node.credential)
		a.node.credential, a.node.session, a.node.execution = credential, sessionID, executionID
		a.node.started, a.node.connected = false, false
		r.credentials[credential] = a.node
	}
	return execution.HostServers{
		Servers: []execution.HostMCPServer{{Name: ServerName, URL: r.url + credential, Headers: map[string]string{}}},
		Secrets: []string{credential},
	}
}

// Started records that the Agent Attach issued the credential for was sent its start.
func (a *Attempt) Started() {
	a.r.mu.Lock()
	defer a.r.mu.Unlock()
	if a.open() {
		a.node.started = true
	}
}

// Unconnected reports whether the node run's current Agent has not asked its endpoint to initialize
// or to list its tools yet. known is false without an Agent that was sent its start: nothing can be
// said about its connection then.
func (r *Registry) Unconnected(nodeRunID string) (unconnected, known bool) {
	if r == nil {
		return false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeRunID]
	if n == nil || !n.started {
		return false, false
	}
	return !n.connected, true
}

// connect records that the Agent holding credential reached its endpoint. One whose credential was
// withdrawn meanwhile says nothing about the Agent that replaced it.
func (r *Registry) connect(credential string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := r.credentials[credential]; n != nil {
		n.connected = true
	}
}

// Instruct issues the token of one sending of the prompt, withdrawing that of an earlier sending
// and any request made with it.
func (a *Attempt) Instruct() execution.HostInstruction {
	r := a.r
	r.mu.Lock()
	defer r.mu.Unlock()
	token := secret()
	if a.open() {
		r.revokeToken(a, RefusedStale)
		a.token, a.signal = token, nil
		r.tokens[token] = a
	}
	return execution.HostInstruction{Token: token, Text: "<workflow_completion>\nWhen this step is complete, call the tool " +
		ToolName + " and pass this attemptToken unchanged: " + token + "\n</workflow_completion>"}
}

// open reports whether the attempt still belongs to its node. Callers hold r.mu.
func (a *Attempt) open() bool { return a.r.nodes[a.node.id] == a.node && a.node.attempts[a.id] == a }

// revokeToken withdraws the attempt's token. Callers hold r.mu.
func (r *Registry) revokeToken(a *Attempt, why Refusal) {
	if a.token == "" {
		return
	}
	delete(r.tokens, a.token)
	if len(r.revoked) >= maxRevoked {
		r.revoked = map[string]Refusal{}
	}
	r.revoked[a.token] = why
	a.token = ""
}

// Take closes the turn attempt of nodeRunID and returns the request the Agent made in it, if any.
// A request arriving later is refused as stale, so it can never attach to another turn.
func (r *Registry) Take(nodeRunID, attempt string) *Signal {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeRunID]
	if n == nil || n.attempts[attempt] == nil {
		return nil
	}
	a := n.attempts[attempt]
	r.revokeToken(a, RefusedStale)
	delete(n.attempts, attempt)
	return a.signal
}

// Revoke withdraws everything of a node run that takes no further turn.
func (r *Registry) Revoke(nodeRunID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.nodes[nodeRunID]
	if n == nil {
		return
	}
	for _, a := range n.attempts {
		r.revokeToken(a, RefusedNotAwaiting)
	}
	delete(r.credentials, n.credential)
	delete(r.nodes, nodeRunID)
}

// holder is what a credential stands for: a node run's Agent, its session and its execution.
type holder struct{ credential, nodeRun, session, execution string }

// authorize returns what a credential stands for.
func (r *Registry) authorize(credential string) (holder, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.credentials[credential]
	if n == nil || credential == "" {
		return holder{}, false
	}
	return holder{credential, n.id, n.session, n.execution}, true
}

// valid reports whether the execution the credential was issued for still runs.
func (r *Registry) valid(ctx context.Context, h holder) (bool, error) {
	running, err := r.state.HostExecutionRunning(ctx, h.execution)
	return running && !r.ending(h.session, h.execution), err
}

// live reports whether the holder may call the tool: its credential is valid and its node executes a
// turn of its session.
func (r *Registry) live(ctx context.Context, h holder) (bool, error) {
	live, err := r.state.WorkflowNodeTurnLive(ctx, h.nodeRun, h.session, h.execution)
	return live && !r.ending(h.session, h.execution), err
}

// accept records the completion request a token authorizes. The token must be the current one of a
// turn of the very node the credential stands for; repeating the call returns the same signal.
func (r *Registry) accept(credential, token string) (Signal, Refusal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.credentials[credential]
	if n == nil {
		return Signal{}, RefusedNotAwaiting
	}
	a := r.tokens[token]
	if a == nil {
		if why, known := r.revoked[token]; known {
			return Signal{}, why
		}
		return Signal{}, RefusedUnknown
	}
	if a.node != n {
		return Signal{}, RefusedUnknown // another node's token is none of this caller's
	}
	if a.signal == nil {
		a.signal = &Signal{ID: secret(), At: r.now()}
	}
	return *a.signal, ""
}

// secret is 128 random bits in hex, so it can sit in a URL path and in JSON text unescaped.
func secret() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	return hex.EncodeToString(b[:])
}
