package controlgrpc

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// agentRunService is the Agent-session side of the internal control contract
// (controller-integration D2..D5). TakeOverThreadEvents is the sole path that persists Node Thread
// events and the sole authority for a run entering `running` (IssueRun D3, plan §4B);
// ClaimThreadCommands and RecordThreadCommandDelivered are the Thread command delivery path (D3,
// plan §4C S3); GrantRevisionUpload is the Revision upload path (Cloud Revision D2/D3).
type agentRunService struct {
	controlpb.UnimplementedAgentRunServiceServer
	store *core.Store
}

// TakeOverThreadEvents persists one batch of settled Node Thread records: Cloud writes the receipts
// and the Thread entries, moves `starting → running` on the first real record, and only then
// answers, which is what lets the Controller ack the batch (protocol root D4). The wire contract
// fixes the batch to 1..64 events in ascending, gap-free order (controller-integration D2); the
// order and continuity decisions belong to the control core, so this layer only converts the wire
// shape to the canonical form the receipts are compared against.
func (s *agentRunService) TakeOverThreadEvents(ctx context.Context, req *controlpb.TakeOverThreadEventsRequest) (*controlpb.TakeOverThreadEventsResponse, error) {
	events := make([]core.Object, 0, len(req.GetEvents()))
	for _, event := range req.GetEvents() {
		converted, e := threadEventObject(event)
		if e != nil {
			return nil, e
		}
		events = append(events, converted)
	}
	body := core.Object{
		"epoch":       req.GetEpoch(),
		"operationId": req.GetOperationId(),
		"executionId": req.GetExecutionId(),
		"events":      events,
	}
	out, e := s.control(ctx, "agent_thread_takeover", req.GetSubmissionId(), body)
	if e != nil {
		return nil, e
	}
	// takenOverThrough is a Node sequence already stored as a positive bigint; the cast cannot lose
	// a sign or a bit above the checked range.
	return &controlpb.TakeOverThreadEventsResponse{TakenOverThrough: uint64(out.N("takenOverThrough"))}, nil // #nosec G115 -- non-negative bounded sequence.
}

// threadEventObject converts one wire ThreadEvent into the canonical object the receipt identity is
// defined on. Every key is always present, so two deliveries of the same event — the C5 replay
// after a lost ack — produce the same canonical form and compare equal, while any difference in the
// record, turn or truncation flag is a genuine payload conflict rather than a formatting artifact.
//
// Validation is the wire contract's own: `record` is a settled `ora-history` line as a JSON object
// of at most 256 KiB ("Cloud stores it verbatim and never rewrites it"; Node protocol D2, Thread
// D2), and `turn_id`, when present, is the Cloud-generated user-turn id the record belongs to.
func threadEventObject(event *controlpb.ThreadEvent) (core.Object, error) {
	if event == nil || event.GetSequence() > math.MaxInt64 || event.GetRecord() == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid_thread_event")
	}
	if len(event.GetRecord()) > threadRecordLimit {
		return nil, status.Error(codes.InvalidArgument, "thread_event_record_too_large")
	}
	var record core.Object
	if e := json.Unmarshal([]byte(event.GetRecord()), &record); e != nil || record == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid_thread_event")
	}
	turnID := event.GetTurnId()
	if turnID != "" {
		if _, e := uuid.Parse(turnID); e != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid_thread_event")
		}
	}
	return core.Object{
		"sequence":  int64(event.GetSequence()), // #nosec G115 -- bounded by the math.MaxInt64 check above.
		"turnId":    turnID,
		"record":    record,
		"truncated": event.GetTruncated(),
	}, nil
}

// threadRecordLimit is the per-record bound of the wire contract (Node protocol D2): a larger
// record is a protocol violation, not a big Thread entry, and is refused before it can reach the
// thread_entries column bound.
const threadRecordLimit = 262144

// ClaimThreadCommands returns the Thread commands the holder may deliver (controller-integration D3).
// The control core decides what is deliverable — not-yet-delivered commands of runs whose session
// execution is already registered — so this layer only converts the answer. The command bodies it
// converts were written by the business layer and validated at enqueue time; a body that no longer
// parses is Cloud's own corrupted state and is surfaced as Internal rather than silently dropped.
func (s *agentRunService) ClaimThreadCommands(ctx context.Context, req *controlpb.ClaimThreadCommandsRequest) (*controlpb.ClaimThreadCommandsResponse, error) {
	out, e := s.control(ctx, "agent_thread_claim", "", core.Object{"epoch": req.GetEpoch(), "limit": int64(req.GetLimit())})
	if e != nil {
		return nil, e
	}
	rows, _ := out["commands"].([]core.Object)
	commands := make([]*controlpb.ThreadCommand, 0, len(rows))
	for _, row := range rows {
		command, e := threadCommand(row)
		if e != nil {
			return nil, e
		}
		commands = append(commands, command)
	}
	return &controlpb.ClaimThreadCommandsResponse{Commands: commands}, nil
}

// RecordThreadCommandDelivered registers the hand-off of one command to the run's Node. It is a state
// change, so it carries the submission identity: a retry after a lost reply replays the recorded
// response, and the first registration is never overwritten by a later one.
func (s *agentRunService) RecordThreadCommandDelivered(ctx context.Context, req *controlpb.RecordThreadCommandDeliveredRequest) (*controlpb.RecordThreadCommandDeliveredResponse, error) {
	body := core.Object{"epoch": req.GetEpoch(), "commandId": req.GetCommandId(), "executionId": req.GetExecutionId()}
	if _, e := s.control(ctx, "agent_thread_delivered", req.GetSubmissionId(), body); e != nil {
		return nil, e
	}
	return &controlpb.RecordThreadCommandDeliveredResponse{}, nil
}

// GrantRevisionUpload answers a Controller's request for the upload grants of one delivery
// execution (Cloud Revision D2/D3). It is a read of the control plane with a cryptographic answer:
// Cloud signs one presigned PUT per object key of the execution's frozen input, and the caller
// forwards the grants to the Node without ever persisting them. Which requests are refused, and with
// which code, is the control core's decision (see revisionUploadGrant); this layer only converts.
func (s *agentRunService) GrantRevisionUpload(ctx context.Context, req *controlpb.GrantRevisionUploadRequest) (*controlpb.GrantRevisionUploadResponse, error) {
	body := core.Object{"epoch": req.GetEpoch(), "executionId": req.GetExecutionId()}
	out, e := s.control(ctx, "agent_revision_grant", "", body)
	if e != nil {
		return nil, e
	}
	rows, _ := out["grants"].([]core.Object)
	grants := make([]*controlpb.UploadGrant, 0, len(rows))
	for _, row := range rows {
		grant, e := uploadGrant(row)
		if e != nil {
			return nil, e
		}
		grants = append(grants, grant)
	}
	return &controlpb.GrantRevisionUploadResponse{Grants: grants}, nil
}

// uploadGrant renders one stored grant as the wire message. Every field is written by Cloud's own
// signer, so a missing or mistyped one is Cloud's corrupted state and is reported as Internal rather
// than sent as an empty capability a Node would fail to use for an unexplained reason.
func uploadGrant(row core.Object) (*controlpb.UploadGrant, error) {
	expires, ok := row["expiresAt"].(time.Time)
	if !ok {
		return nil, status.Error(codes.Internal, "invalid_upload_grant")
	}
	headers := make(map[string]string, len(row.O("headers")))
	for name, value := range row.O("headers") {
		text, ok := value.(string)
		if !ok {
			return nil, status.Error(codes.Internal, "invalid_upload_grant")
		}
		headers[name] = text
	}
	return &controlpb.UploadGrant{
		ObjectKey: row.S("objectKey"),
		Url:       row.S("url"),
		Method:    row.S("method"),
		Headers:   headers,
		ExpiresAt: timestamppb.New(expires),
	}, nil
}

// threadCommand renders one stored command row as the wire message. The durable body is the
// canonical JSON shape the business layer produced; the wire shape is the proto oneof, so the
// content-block translation lives here and nowhere else — the mirror of input()/result() in
// executions.go.
func threadCommand(row core.Object) (*controlpb.ThreadCommand, error) {
	body := row.O("body")
	out := &controlpb.ThreadCommand{
		CommandId:   row.S("id"),
		RunId:       row.S("runId"),
		ExecutionId: row.S("executionId"),
		Target:      &controlpb.WorkTarget{NodeId: row.S("nodeId")},
	}
	switch row.S("kind") {
	case "submit_user_turn":
		blocks, e := contentBlocks(body.O("turn")["content"])
		if e != nil {
			return nil, e
		}
		out.Command = &controlpb.ThreadCommand_SubmitUserTurn{SubmitUserTurn: &controlpb.SubmitUserTurn{
			Turn: &controlpb.UserTurn{TurnId: body.O("turn").S("turn_id"), Content: blocks},
		}}
	case "end_session":
		reason, ok := controlpb.EndSessionReason_value["END_SESSION_REASON_"+strings.ToUpper(body.S("reason"))]
		if !ok {
			return nil, status.Error(codes.Internal, "stored_thread_command_invalid")
		}
		out.Command = &controlpb.ThreadCommand_EndSession{EndSession: &controlpb.EndSession{Reason: controlpb.EndSessionReason(reason)}}
	default:
		return nil, status.Error(codes.Internal, "stored_thread_command_invalid")
	}
	return out, nil
}

// contentBlocks converts the stored content list to the wire blocks. Only text blocks can be stored
// (the public contract accepts nothing else), so anything else is Cloud's own corrupted state and is
// reported rather than silently dropped — dropping would lose user content.
func contentBlocks(v any) ([]*controlpb.ContentBlock, error) {
	rows, ok := v.([]any)
	if !ok || len(rows) == 0 {
		return nil, status.Error(codes.Internal, "stored_thread_command_invalid")
	}
	blocks := make([]*controlpb.ContentBlock, 0, len(rows))
	for _, row := range rows {
		block, ok := row.(map[string]any)
		text, isText := block["text"].(string)
		if !ok || !isText || block["type"] != "text" {
			return nil, status.Error(codes.Internal, "stored_thread_command_invalid")
		}
		blocks = append(blocks, &controlpb.ContentBlock{Block: &controlpb.ContentBlock_Text{Text: &controlpb.TextContent{Text: text}}})
	}
	return blocks, nil
}

// control runs one Agent-run action for the verified principal and maps its Fault.
func (s *agentRunService) control(ctx context.Context, action, submission string, body core.Object) (core.Object, error) {
	out, e := s.store.Control(ctx, &core.ControlRequest{Action: action, SubmissionID: submission, Body: body, Service: principal(ctx)})
	if e != nil {
		return nil, toStatus(e)
	}
	return out, nil
}
