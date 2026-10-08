package controlgrpc

import (
	"context"
	"encoding/base64"
	"math"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// executionService is the execution registry of the clone loop. Each RPC is one clone_* control
// action; state-changing calls carry their submission identity so Cloud replays a recorded
// response instead of reapplying when the Controller retries after a lost reply.
type executionService struct {
	controlpb.UnimplementedExecutionServiceServer
	store *core.Store
}

func (s *executionService) ClaimWork(ctx context.Context, req *controlpb.ClaimWorkRequest) (*controlpb.ClaimWorkResponse, error) {
	out, e := s.control(ctx, "clone_claim", "", core.Object{"epoch": req.GetEpoch()})
	if e != nil {
		return nil, e
	}
	request := out.O("request")
	if len(request) == 0 {
		return &controlpb.ClaimWorkResponse{}, nil
	}
	return &controlpb.ClaimWorkResponse{Item: &controlpb.WorkItem{OperationId: request.S("id"), Input: input("clone", core.Object{"repositoryUrl": request.S("repositoryUrl"), "branch": request.S("branch")})}}, nil
}

func (s *executionService) RecordDispatch(ctx context.Context, req *controlpb.RecordDispatchRequest) (*controlpb.RecordDispatchResponse, error) {
	in, e := inputObject(req.GetInput())
	if e != nil {
		return nil, e
	}
	body := core.Object{"epoch": req.GetEpoch(), "operationId": req.GetOperationId(), "executionId": req.GetExecutionId(), "nodeId": req.GetNodeId(), "input": in}
	out, e := s.control(ctx, "clone_dispatch", req.GetSubmissionId(), body)
	if e != nil {
		return nil, e
	}
	return &controlpb.RecordDispatchResponse{Record: record(out)}, nil
}

func (s *executionService) TakeOverNodeEvent(ctx context.Context, req *controlpb.TakeOverNodeEventRequest) (*controlpb.TakeOverNodeEventResponse, error) {
	res, e := resultObject(req.GetResult())
	if e != nil {
		return nil, e
	}
	sequence, ok := receiptSequence(req.GetSequence())
	if !ok || len(req.GetEvent()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid_receipt")
	}
	// Two execution kinds report terminal facts through this one RPC, and each has its own takeover
	// action: a session execution's terminal event ends the Thread (controller-integration D2/D6),
	// every other execution's result is the clone/delivery terminal path. The outcome decides, so a
	// session terminal event can never be committed as a clone result, or the reverse.
	//
	// The two paths also store their receipt differently, and deliberately so. clone_event_receipts
	// holds the receipt as text, so the clone path stores base64 of the Node's own bytes. A session
	// terminal event is receipted in node_event_receipts, whose `event` is a jsonb object compared by
	// canonical value (D-022) — and a session execution's terminal event is not a ThreadEvent, so it
	// has no `record` to canonicalize from: its payload is the terminal result, and the raw Node bytes
	// are kept verbatim beside it because that is what request.event promises a conflict is detected
	// on. Both forms are computed from the same request, so neither can drift from the other.
	action := "clone_takeover"
	var event any = base64.StdEncoding.EncodeToString(req.GetEvent())
	if res.S("outcome") == agentSessionEndedOutcome {
		action = "agent_session_takeover"
		event = core.Object{"sequence": sequence, "result": res, "event": base64.StdEncoding.EncodeToString(req.GetEvent())}
	}
	if deliveryOutcomes[res.S("outcome")] {
		// A delivery execution's terminal result is receipted like a session's and for the same
		// reason: node_event_receipts.event is a jsonb object compared by canonical value, and a
		// delivery result is not a ThreadEvent, so it has no `record` to canonicalize from. The raw
		// Node bytes ride beside it because that is what the contract promises a conflict is detected
		// on.
		action = "agent_delivery_takeover"
		event = core.Object{"sequence": sequence, "result": res, "event": base64.StdEncoding.EncodeToString(req.GetEvent())}
	}
	body := core.Object{"epoch": req.GetEpoch(), "operationId": req.GetOperationId(), "executionId": req.GetExecutionId(), "sequence": sequence, "result": res, "event": event}
	out, e := s.control(ctx, action, req.GetSubmissionId(), body)
	if e != nil {
		return nil, e
	}
	return &controlpb.TakeOverNodeEventResponse{Record: record(out)}, nil
}

func (s *executionService) RecordQueriedResult(ctx context.Context, req *controlpb.RecordQueriedResultRequest) (*controlpb.RecordQueriedResultResponse, error) {
	res, e := resultObject(req.GetResult())
	if e != nil {
		return nil, e
	}
	body := core.Object{"epoch": req.GetEpoch(), "operationId": req.GetOperationId(), "executionId": req.GetExecutionId(), "result": res}
	out, e := s.control(ctx, "clone_queried", req.GetSubmissionId(), body)
	if e != nil {
		return nil, e
	}
	return &controlpb.RecordQueriedResultResponse{Record: record(out)}, nil
}

func (s *executionService) GetDispatch(ctx context.Context, req *controlpb.GetDispatchRequest) (*controlpb.GetDispatchResponse, error) {
	out, e := s.control(ctx, "clone_get", "", core.Object{"executionId": req.GetExecutionId()})
	if e != nil {
		return nil, e
	}
	return &controlpb.GetDispatchResponse{Record: record(out)}, nil
}

func (s *executionService) ListPendingDispatches(ctx context.Context, req *controlpb.ListPendingDispatchesRequest) (*controlpb.ListPendingDispatchesResponse, error) {
	out, e := s.control(ctx, "clone_pending", "", core.Object{"nodeId": req.GetNodeId()})
	if e != nil {
		return nil, e
	}
	rows, _ := out["executions"].([]core.Object)
	records := make([]*controlpb.ExecutionRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, record(row))
	}
	return &controlpb.ListPendingDispatchesResponse{Records: records}, nil
}

// control runs one clone action for the verified principal and maps its Fault.
func (s *executionService) control(ctx context.Context, action, submission string, body core.Object) (core.Object, error) {
	out, e := s.store.Control(ctx, &core.ControlRequest{Action: action, SubmissionID: submission, Body: body, Service: principal(ctx)})
	if e != nil {
		return nil, toStatus(e)
	}
	return out, nil
}

// receiptSequence narrows the wire sequence to the bigint column; anything larger is not a Node
// sequence this contract can store and is rejected instead of wrapping.
func receiptSequence(sequence uint64) (int64, bool) {
	if sequence > math.MaxInt64 {
		return 0, false
	}
	return int64(sequence), true // #nosec G115 -- bounded above.
}

// The JSON shapes below are the durable form of the contract messages; they are what
// clone_executions.input / result hold and what conflicts are compared against.

func inputObject(in *controlpb.ExecutionInput) (core.Object, error) {
	clone := in.GetClone()
	if clone == nil || clone.GetRepository() == "" || clone.GetBranch() == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid_dispatch")
	}
	return core.Object{"kind": "clone", "repositoryUrl": clone.GetRepository(), "branch": clone.GetBranch()}, nil
}

// agentSessionEndedOutcome and agentSessionEndReasonNames are the wire's own spelling of the
// session-terminal outcome. They mirror the proto enum rather than a core constant, because
// translating enum ↔ stored name is exactly this layer's job; a name outside the approved enum is
// never produced here, so an unknown reason cannot be stored as if it were a contract value.
const agentSessionEndedOutcome = "agent_session_ended"

// agentSessionEndReasonName renders the enum's own name in the stored snake_case form
// (`user_ended`, `idle_timeout`, …). UNSPECIFIED is not a reason a Node may report, and is refused
// instead of being stored as a value no IssueRun rule handles.
func agentSessionEndReasonName(r controlpb.AgentSessionEndReason) (string, bool) {
	if r == controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_UNSPECIFIED {
		return "", false
	}
	name := strings.TrimPrefix(r.String(), "AGENT_SESSION_END_REASON_")
	if name == r.String() {
		return "", false
	}
	return strings.ToLower(name), true
}

// Delivery terminal outcomes, the wire's spelling of the proto messages RevisionDelivered /
// RevisionUnchanged / RevisionFailed. They mirror the message names rather than a core constant
// because translating the wire shape is exactly this layer's job (the core re-validates the reduced
// outcome against its own closed set).
const (
	revisionDeliveredOutcome = "revision_delivered"
	revisionUnchangedOutcome = "revision_unchanged"
	revisionFailedOutcome    = "revision_failed"
)

// deliveryOutcomes is the set that routes a takeover to the delivery path.
var deliveryOutcomes = map[string]bool{
	revisionDeliveredOutcome: true,
	revisionUnchangedOutcome: true,
	revisionFailedOutcome:    true,
}

// revisionFailureReasonName renders RevisionFailureReason's own name in the stored snake_case form.
// Two values are refused rather than stored: UNSPECIFIED is not a reason, and VERIFICATION_FAILED is
// the verdict Cloud records when the declared objects are missing or differ — the proto says "a Node
// never reports it", so accepting it here would let a Node assert a verification Cloud never ran.
func revisionFailureReasonName(r controlpb.RevisionFailureReason) (string, bool) {
	switch r {
	case controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UNSPECIFIED,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_VERIFICATION_FAILED:
		return "", false
	}
	name := strings.TrimPrefix(r.String(), "REVISION_FAILURE_REASON_")
	if name == r.String() {
		return "", false
	}
	return strings.ToLower(name), true
}

// revisionVerificationFailureReason is the stored spelling of Cloud's own verification verdict. It
// mirrors core's unexported `revisionVerificationFailure`: the two must name the same value, because
// core writes the reason and this layer reads it back.
const revisionVerificationFailureReason = "verification_failed"

// revisionFailureReasonEnum is the inverse of revisionFailureReasonName for rendering a stored
// result back to the wire. An unrecognized stored name is UNSPECIFIED rather than a guess.
//
// It is deliberately WIDER than the parser on exactly one value. `verification_failed` is Cloud's own
// verdict, recorded when a delivery's declared objects could not be confirmed (Cloud Revision D4
// step 4): no Node may report it, which is what revisionFailureReasonName enforces, but Cloud stores
// it and the Controller must be able to read it. Projecting it through the parser would render every
// verification failure as UNSPECIFIED — a Controller could not tell "the objects were missing" from
// "no reason given", which is the difference between a retry worth attempting and one that is not.
func revisionFailureReasonEnum(name string) controlpb.RevisionFailureReason {
	if name == revisionVerificationFailureReason {
		return controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_VERIFICATION_FAILED
	}
	for _, r := range controlpb.RevisionFailureReason_value {
		if stored, ok := revisionFailureReasonName(controlpb.RevisionFailureReason(r)); ok && stored == name {
			return controlpb.RevisionFailureReason(r)
		}
	}
	return controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UNSPECIFIED
}

// agentSessionEndReasonEnum is the inverse of agentSessionEndReasonName for rendering a stored
// result back to the wire. An unrecognized stored name is UNSPECIFIED rather than a guess.
func agentSessionEndReasonEnum(name string) controlpb.AgentSessionEndReason {
	for _, r := range controlpb.AgentSessionEndReason_value {
		if stored, ok := agentSessionEndReasonName(controlpb.AgentSessionEndReason(r)); ok && stored == name {
			return controlpb.AgentSessionEndReason(r)
		}
	}
	return controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_UNSPECIFIED
}

// storedObject renders a Node's measurement of one uploaded object into the stored result. A nil
// message is stored as an absent key: `revision_unchanged` legitimately carries no bundle, and
// inventing a zero-valued object would claim an upload of nothing.
//
// The size is converted from the wire's `uint64` to the exact integer every consumer reads it with
// (`core.Object.N` recognizes int64/float64/int/json.Number and answers 0 for anything else) — a
// stored `uint64` would read back as a size of zero, and Cloud would then verify the object store
// against a declaration the Node never made. A value beyond int64 is not a possible length and is
// refused here rather than truncated into a smaller one Cloud would faithfully verify.
func storedObject(o *controlpb.StoredObject) (core.Object, error) {
	if o == nil {
		return nil, nil
	}
	size := o.GetSize()
	if size > math.MaxInt64 {
		return nil, status.Error(codes.InvalidArgument, "invalid_result")
	}
	//nolint:gosec // G115: `size` is proven in int64's range by the bound above, which gosec cannot see.
	return core.Object{"key": o.GetKey(), "size": int64(size), "sha256": o.GetSha256()}, nil
}

// storedObjectOf is the inverse of storedObject for rendering a stored measurement back to the wire.
// An absent key yields nil rather than a zero-valued message, so the two directions round-trip.
//
// The size is converted through an explicit non-negative check because the stored JSON number is
// read as a signed int64 while the wire field is unsigned: every value storedObject wrote came from
// a uint64, so a negative one is unrepresentable rather than meaningful, and clamping keeps this
// projection total instead of letting a corrupt row wrap to a huge byte count.
func storedObjectOf(o core.Object) *controlpb.StoredObject {
	if len(o) == 0 {
		return nil
	}
	size := o.N("size")
	if size < 0 {
		size = 0
	}
	return &controlpb.StoredObject{Key: o.S("key"), Size: uint64(size), Sha256: o.S("sha256")}
}

// input renders one stored execution_work/dispatch input object back to the wire. kind is the
// execution's kind ("" for clone executions, whose registry has no such column): without it an
// Agent session or delivery record would be rendered as an empty CloneSpec, which is a misleading
// shape rather than a missing one. The stored JSONB stays authoritative — this projection is for a
// Controller that recovers a dispatch, and no reader parses it back.
func input(kind string, o core.Object) *controlpb.ExecutionInput {
	switch kind {
	case "agent_session":
		turn := o.O("initial_turn")
		spec := &controlpb.AgentSessionSpec{
			AgentPluginId:       o.S("agent_plugin_id"),
			AgentPluginVersion:  o.S("agent_plugin_version"),
			CheckoutExecutionId: o.S("checkout_execution_id"),
			InitialTurn:         &controlpb.UserTurn{TurnId: turn.S("turn_id")},
		}
		// The B-side snapshot renders the first prompt as one text string (AgentSessionWork); it is
		// projected as a single text block so the shape is the contract's, not a reinterpretation.
		if text, ok := turn["content"].(string); ok {
			spec.InitialTurn.Content = []*controlpb.ContentBlock{{Block: &controlpb.ContentBlock_Text{Text: &controlpb.TextContent{Text: text}}}}
		}
		return &controlpb.ExecutionInput{Spec: &controlpb.ExecutionInput_AgentSession{AgentSession: spec}}
	case "deliver_revision":
		return &controlpb.ExecutionInput{Spec: &controlpb.ExecutionInput_DeliverRevision{DeliverRevision: &controlpb.DeliverRevisionSpec{
			SessionExecutionId:  o.S("session_execution_id"),
			CheckoutExecutionId: o.S("checkout_execution_id"),
			BaseCommit:          o.S("base_commit"),
			RevisionRef:         o.S("revision_ref"),
			BundleKey:           o.S("bundle_key"),
			HistoryKey:          o.S("history_key"),
		}}}
	default:
		return &controlpb.ExecutionInput{Spec: &controlpb.ExecutionInput_Clone{Clone: &controlpb.CloneSpec{Repository: o.S("repositoryUrl"), Branch: o.S("branch")}}}
	}
}

func resultObject(res *controlpb.ExecutionResult) (core.Object, error) {
	node := res.GetNode()
	if node == nil || node.GetNodeId() == "" || node.GetNodeIncarnationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid_result")
	}
	out := core.Object{"node": core.Object{"nodeId": node.GetNodeId(), "nodeIncarnationId": node.GetNodeIncarnationId()}}
	switch outcome := res.GetOutcome().(type) {
	case *controlpb.ExecutionResult_CloneReady:
		out["outcome"], out["path"], out["commit"] = "clone_ready", outcome.CloneReady.GetPath(), outcome.CloneReady.GetCommit()
	case *controlpb.ExecutionResult_CloneFailed:
		out["outcome"], out["reason"] = "clone_failed", outcome.CloneFailed.GetReason().String()
		if outcome.CloneFailed.RetainedPath != nil {
			out["retainedPath"] = outcome.CloneFailed.GetRetainedPath()
		}
	case *controlpb.ExecutionResult_AgentSessionEnded:
		reason, ok := agentSessionEndReasonName(outcome.AgentSessionEnded.GetReason())
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "invalid_result")
		}
		out["outcome"], out["reason"] = agentSessionEndedOutcome, reason
		// `detail` is a bounded code, never raw Agent output; it is stored only when the Node sent it.
		if outcome.AgentSessionEnded.Detail != nil {
			out["detail"] = outcome.AgentSessionEnded.GetDetail()
		}
	case *controlpb.ExecutionResult_RevisionDelivered:
		// The Node's own measurement of what it uploaded. Cloud does not trust it as a verdict — the
		// Revision decision has Cloud verify the objects before registering — so this layer stores the
		// claim verbatim and verifies nothing.
		d := outcome.RevisionDelivered
		out["outcome"] = revisionDeliveredOutcome
		out["finalCommit"], out["baseCommit"], out["revisionRef"] = d.GetFinalCommit(), d.GetBaseCommit(), d.GetRevisionRef()
		bundle, err := storedObject(d.GetBundle())
		if err != nil {
			return nil, err
		}
		history, err := storedObject(d.GetHistory())
		if err != nil {
			return nil, err
		}
		out["bundle"], out["history"] = bundle, history
	case *controlpb.ExecutionResult_RevisionUnchanged:
		// The final commit equals the base commit: no bundle, only the session history.
		u := outcome.RevisionUnchanged
		out["outcome"] = revisionUnchangedOutcome
		out["finalCommit"], out["baseCommit"], out["revisionRef"] = u.GetFinalCommit(), u.GetBaseCommit(), u.GetRevisionRef()
		history, err := storedObject(u.GetHistory())
		if err != nil {
			return nil, err
		}
		out["history"] = history
	case *controlpb.ExecutionResult_RevisionFailed:
		reason, ok := revisionFailureReasonName(outcome.RevisionFailed.GetReason())
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "invalid_result")
		}
		out["outcome"], out["reason"] = revisionFailedOutcome, reason
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid_result")
	}
	return out, nil
}

func result(o core.Object) *controlpb.ExecutionResult {
	node := o.O("node")
	out := &controlpb.ExecutionResult{Node: &controlpb.NodeIdentity{NodeId: node.S("nodeId"), NodeIncarnationId: node.S("nodeIncarnationId")}}
	switch o.S("outcome") {
	case "clone_ready":
		out.Outcome = &controlpb.ExecutionResult_CloneReady{CloneReady: &controlpb.CloneReady{Path: o.S("path"), Commit: o.S("commit")}}
		return out
	case agentSessionEndedOutcome:
		ended := &controlpb.AgentSessionEnded{Reason: agentSessionEndReasonEnum(o.S("reason"))}
		if detail, ok := o["detail"].(string); ok {
			ended.Detail = &detail
		}
		out.Outcome = &controlpb.ExecutionResult_AgentSessionEnded{AgentSessionEnded: ended}
		return out
	case revisionDeliveredOutcome:
		out.Outcome = &controlpb.ExecutionResult_RevisionDelivered{RevisionDelivered: &controlpb.RevisionDelivered{
			FinalCommit: o.S("finalCommit"), BaseCommit: o.S("baseCommit"), RevisionRef: o.S("revisionRef"),
			Bundle: storedObjectOf(o.O("bundle")), History: storedObjectOf(o.O("history")),
		}}
		return out
	case revisionUnchangedOutcome:
		out.Outcome = &controlpb.ExecutionResult_RevisionUnchanged{RevisionUnchanged: &controlpb.RevisionUnchanged{
			FinalCommit: o.S("finalCommit"), BaseCommit: o.S("baseCommit"), RevisionRef: o.S("revisionRef"),
			History: storedObjectOf(o.O("history")),
		}}
		return out
	case revisionFailedOutcome:
		out.Outcome = &controlpb.ExecutionResult_RevisionFailed{RevisionFailed: &controlpb.RevisionFailed{
			Reason: revisionFailureReasonEnum(o.S("reason")),
		}}
		return out
	default:
		failed := &controlpb.CloneFailed{Reason: controlpb.CloneFailureReason(controlpb.CloneFailureReason_value[o.S("reason")])}
		if retained, ok := o["retainedPath"].(string); ok {
			failed.RetainedPath = &retained
		}
		out.Outcome = &controlpb.ExecutionResult_CloneFailed{CloneFailed: failed}
		return out
	}
}

func record(row core.Object) *controlpb.ExecutionRecord {
	out := &controlpb.ExecutionRecord{OperationId: row.S("operationId"), ExecutionId: row.S("executionId"), NodeId: row.S("nodeId"), Input: input(row.S("kind"), row.O("input"))}
	if res := row.O("result"); len(res) > 0 {
		out.Result = result(res)
	}
	return out
}
