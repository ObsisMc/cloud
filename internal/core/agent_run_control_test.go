package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// stubAgentRunControlPlane is a deterministic, in-memory fake for the A-seam contract, used only by
// unit tests. It records every call and returns configured results, with no database, goroutine, sleep,
// timer, or HTTP involved. It proves the contract can be injected into a Store and observes the
// caller-owned transaction handle (the fake never opens one).
type stubAgentRunControlPlane struct {
	busy       bool
	accepted   bool
	createErr  error
	workID     string
	workErr    error
	commandID  string
	commandErr error
	deleteErr  error

	mu         sync.Mutex
	createTx   *transaction
	createRun  Object
	createN    int
	deleteTx   *transaction
	deleteRun  Object
	deleteN    int
	workTx     *transaction
	workRun    Object
	workKind   string
	workIn     Object
	workTarget Object
	workAt     *time.Time
	workN      int
	commandTx  *transaction
	commandRun Object
	commandCmd Object
	commandN   int
}

func (s *stubAgentRunControlPlane) CreateRunWorkspace(t *transaction, run Object) (RunWorkspaceOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createTx, s.createRun, s.createN = t, run, s.createN+1
	return RunWorkspaceOutcome{Accepted: s.accepted, Busy: s.busy}, s.createErr
}

func (s *stubAgentRunControlPlane) DeleteRunWorkspace(t *transaction, run Object) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteTx, s.deleteRun, s.deleteN = t, run, s.deleteN+1
	return s.deleteErr
}

func (s *stubAgentRunControlPlane) EnqueueExecutionWork(t *transaction, run Object, kind string, input, target Object, availableAt *time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workTx, s.workRun, s.workKind, s.workIn, s.workTarget, s.workAt, s.workN = t, run, kind, input, target, availableAt, s.workN+1
	return s.workID, s.workErr
}

func (s *stubAgentRunControlPlane) EnqueueThreadCommand(t *transaction, run, command Object) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commandTx, s.commandRun, s.commandCmd, s.commandN = t, run, command, s.commandN+1
	return s.commandID, s.commandErr
}

// UnavailableAgentRunControlPlane must be safely assignable to the seam interface.
var (
	_ AgentRunControlPlane = UnavailableAgentRunControlPlane{}
	_ AgentRunControlPlane = (*stubAgentRunControlPlane)(nil)
)

// The control-plane default must fail closed while the A side is unwired.
func TestAgentRunControlUnavailableFailsClosed(t *testing.T) {
	uc := UnavailableAgentRunControlPlane{}
	for name, got := range map[string]error{
		"createRunWorkspace": func() error { _, e := uc.CreateRunWorkspace(nil, Object{}); return e }(),
		"deleteRunWorkspace": func() error { return uc.DeleteRunWorkspace(nil, Object{}) }(),
		"enqueueExecutionWork": func() error {
			_, e := uc.EnqueueExecutionWork(nil, Object{}, "agent_session", Object{}, Object{}, nil)
			return e
		}(),
		"enqueueThreadCommand": func() error { _, e := uc.EnqueueThreadCommand(nil, Object{}, Object{}); return e }(),
	} {
		if got == nil {
			t.Fatalf("%s: unavailable seam must fail closed, got nil error", name)
		}
		if got.Error() == "" {
			t.Fatalf("%s: unavailable seam error must be non-empty", name)
		}
	}
}

// The nil-safe accessor fails closed by default and honors an injected provider.
func TestStoreAgentRunControlPlaneNilSafe(t *testing.T) {
	s := &Store{}
	if _, ok := s.agentRunControlPlane().(UnavailableAgentRunControlPlane); !ok {
		t.Fatalf("default agentRunControlPlane must be Unavailable, got %T", s.agentRunControlPlane())
	}
	stub := &stubAgentRunControlPlane{accepted: true}
	s.AgentRunControlPlane = stub
	if s.agentRunControlPlane() != stub {
		t.Fatalf("injected agentRunControlPlane not returned")
	}
}

// createRunWorkspace must distinguish accepted and busy as values (not errors) and must observe the
// caller-owned transaction handle.
func TestAgentRunCreateRunWorkspaceOutcomes(t *testing.T) {
	stub := &stubAgentRunControlPlane{accepted: true}
	tx := &transaction{ctx: context.Background()}
	out, err := stub.CreateRunWorkspace(tx, Object{"id": "run-1"})
	if out.Accepted != true || out.Busy != false || err != nil {
		t.Fatalf("accepted outcome = %+v, %v", out, err)
	}
	if stub.createN != 1 || stub.createRun.S("id") != "run-1" || stub.createTx != tx {
		t.Fatalf("accepted call not recorded with the caller's transaction: n=%d run=%v tx=%p", stub.createN, stub.createRun, stub.createTx)
	}

	busyStub := &stubAgentRunControlPlane{busy: true}
	out, err = busyStub.CreateRunWorkspace(tx, Object{"id": "run-2"})
	// Busy must be expressible as a value with a nil error, never forced through the error channel.
	if out.Busy != true || out.Accepted != false || err != nil {
		t.Fatalf("busy must be a value outcome with nil error, got %+v, %v", out, err)
	}

	failStub := &stubAgentRunControlPlane{createErr: context.DeadlineExceeded}
	if _, err = failStub.CreateRunWorkspace(tx, Object{}); err == nil {
		t.Fatal("real failure must surface as an error")
	}
}

// The remaining seams must compile against the contract, record their identity/payload, and pass the
// caller-owned transaction through.
func TestAgentRunRemainingSeamContract(t *testing.T) {
	stub := &stubAgentRunControlPlane{workID: "work-1", commandID: "cmd-1"}
	tx := &transaction{ctx: context.Background()}

	if e := stub.DeleteRunWorkspace(tx, Object{"id": "run-1"}); e != nil {
		t.Fatalf("deleteRunWorkspace: %v", e)
	}
	if stub.deleteN != 1 || stub.deleteTx != tx || stub.deleteRun.S("id") != "run-1" {
		t.Fatalf("deleteRunWorkspace not recorded: n=%d tx=%p", stub.deleteN, stub.deleteTx)
	}

	work := AgentSessionWork{AgentPluginID: "official/hello-world", AgentPluginVersion: "1.0.0", InitialTurn: Object{"content": "hi"}}
	at := time.Now()
	workID, e := stub.EnqueueExecutionWork(tx, Object{"id": "run-1", "workspaceId": "ws-1"}, "agent_session", work.inputObject(), Object{"type": "node"}, &at)
	if workID != "work-1" || e != nil {
		t.Fatalf("enqueueExecutionWork = %q, %v", workID, e)
	}
	if stub.workN != 1 || stub.workTx != tx || stub.workRun.S("id") != "run-1" || stub.workRun.S("workspaceId") != "ws-1" {
		t.Fatalf("enqueueExecutionWork not recorded: n=%d tx=%p run=%v", stub.workN, stub.workTx, stub.workRun)
	}
	if stub.workKind != "agent_session" || stub.workAt != &at {
		t.Fatalf("enqueueExecutionWork kind/availableAt not passed: kind=%q at=%p", stub.workKind, stub.workAt)
	}
	snapshot := stub.workIn
	if snapshot.S("agent_plugin_id") != "official/hello-world" || snapshot.S("agent_plugin_version") != "1.0.0" {
		t.Fatalf("snapshotted plugin identity/version must flow into the work input untouched: %v", snapshot)
	}

	cmdID, e := stub.EnqueueThreadCommand(tx, Object{"id": "run-1"}, Object{"kind": "SubmitUserTurn"})
	if cmdID != "cmd-1" || e != nil {
		t.Fatalf("enqueueThreadCommand = %q, %v", cmdID, e)
	}
	if stub.commandN != 1 || stub.commandTx != tx || stub.commandCmd.S("kind") != "SubmitUserTurn" {
		t.Fatalf("enqueueThreadCommand not recorded: n=%d tx=%p cmd=%v", stub.commandN, stub.commandTx, stub.commandCmd)
	}
}

// The AgentSessionWork typed container renders the create-time snapshot into the exact JSONBObject the
// control plane stores, so the future A implementation never re-reads the current plugin version.
func TestAgentSessionWorkSnapshot(t *testing.T) {
	w := AgentSessionWork{AgentPluginID: "official/hello-world", AgentPluginVersion: "1.0.0", InitialTurn: Object{"turnId": "t-1", "content": "do it"}}
	o := w.inputObject()
	if o.S("agent_plugin_id") != "official/hello-world" || o.S("agent_plugin_version") != "1.0.0" {
		t.Fatalf("snapshot rendered wrong: %v", o)
	}
	turn := o.O("initial_turn")
	if turn.S("turnId") != "t-1" || turn.S("content") != "do it" {
		t.Fatalf("initial turn not carried: %v", turn)
	}
}
