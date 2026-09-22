package core

import (
	"context"
	"strings"
)

// EnqueueClone accepts a clone request in Cloud's own transaction and hands it to Controllers as
// queued work. Repeating (tenant, user, requestId) with the same input returns the original
// request; different input is a conflict. Nothing is dispatched here: a Controller claims it.
func (s *Store) EnqueueClone(ctx context.Context, tenantID, userID, requestID, repositoryURL, branch string) (Object, error) {
	out, err := s.transact(ctx, func(t *transaction) Object {
		require(requestID != "" && repositoryURL != "" && branch != "", 400, "invalid_clone_request")
		membership(t, tenantID, userID, false)
		if existing := t.one("SELECT * FROM clone_requests WHERE tenant_id=$1 AND actor_user_id=$2 AND request_id=$3", tenantID, userID, requestID); existing != nil {
			require(existing.S("repositoryUrl") == repositoryURL && existing.S("branch") == branch, 409, "idempotency_conflict")
			return existing
		}
		id := newID()
		t.exec("INSERT INTO clone_requests(id,tenant_id,actor_user_id,request_id,repository_url,branch,state) VALUES($1,$2,$3,$4,$5,$6,'queued')", id, tenantID, userID, requestID, repositoryURL, branch)
		return t.one("SELECT * FROM clone_requests WHERE id=$1", id)
	})
	// Signal only after commit so a Controller that claims immediately finds the row.
	if err == nil && out.S("state") == "queued" && s.Signals != nil {
		s.Signals.Publish(ControlSignal{Kind: SignalWorkAvailable, OperationID: out.S("id")})
	}
	return out, err
}

// submitted wraps one state-changing control action in its submission identity. The identity is
// checked and recorded inside the caller's transaction, so a recorded response and its effects
// commit together: a retry after a lost reply replays the response, never the effect.
func submitted(t *transaction, r *ControlRequest, apply func() Object) Object {
	require(len(r.SubmissionID) <= 200, 400, "invalid_submission")
	if r.SubmissionID == "" {
		return apply()
	}
	hash := requestHash(r.Action, r.Service.Subject, r.Body)
	if old := t.one("SELECT * FROM control_submissions WHERE submission_id=$1", r.SubmissionID); old != nil {
		require(old.S("requestHash") == hash, 409, "submission_conflict")
		return old.O("response")
	}
	out := apply()
	t.exec("INSERT INTO control_submissions(submission_id,holder_id,request_hash,response) VALUES($1,$2,$3,$4)", r.SubmissionID, r.Service.Subject, hash, jsonText(out))
	return out
}

// cloneCommand runs the execution-centric control actions. Lease validity was already checked;
// the epoch is recorded on dispatch so a stale worker's later writes are fenced by their own epoch.
func cloneCommand(t *transaction, r *ControlRequest) Object {
	switch r.Action {
	case "clone_claim":
		// A pure read: ownership moves only when the dispatch is recorded, so a Controller that
		// dies between claim and dispatch leaves nothing to recover.
		work := t.one("SELECT * FROM clone_requests WHERE state='queued' ORDER BY created_at,id LIMIT 1")
		return Object{"request": work}
	case "clone_dispatch":
		return submitted(t, r, func() Object { return cloneDispatch(t, r) })
	case "clone_takeover":
		return submitted(t, r, func() Object { return cloneResult(t, r, true) })
	case "clone_queried":
		return submitted(t, r, func() Object { return cloneResult(t, r, false) })
	case "clone_get":
		e := t.one("SELECT * FROM clone_executions WHERE execution_id=$1", r.Body.S("executionId"))
		require(e != nil, 404, "not_found")
		return e
	case "clone_pending":
		node := r.Body.S("nodeId")
		if node == "" {
			return Object{"executions": t.list("SELECT * FROM clone_executions WHERE result IS NULL ORDER BY created_at,execution_id")}
		}
		return Object{"executions": t.list("SELECT * FROM clone_executions WHERE result IS NULL AND node_id=$1 ORDER BY created_at,execution_id", node)}
	default:
		reject(404, "not_found")
	}
	return nil
}

// cloneDispatch registers execution identity, target Node and full input before any Node sees the
// command. The request must still be queued (or already dispatched as exactly this execution).
func cloneDispatch(t *transaction, r *ControlRequest) Object {
	operation, execution, node := r.Body.S("operationId"), r.Body.S("executionId"), r.Body.S("nodeId")
	input := r.Body.O("input")
	require(validID(operation) && execution != "" && node != "" && len(input) > 0, 400, "invalid_dispatch")
	request := t.one("SELECT * FROM clone_requests WHERE id=$1", operation)
	require(request != nil, 404, "not_found")
	require(input.S("repositoryUrl") == request.S("repositoryUrl") && input.S("branch") == request.S("branch"), 409, "dispatch_conflict")
	if existing := t.one("SELECT * FROM clone_executions WHERE operation_id=$1", operation); existing != nil {
		require(existing.S("executionId") == execution && existing.S("nodeId") == node && jsonText(existing.O("input")) == jsonText(input), 409, "dispatch_conflict")
		return existing
	}
	require(request.S("state") == "queued", 409, "dispatch_conflict")
	require(t.one("SELECT execution_id FROM clone_executions WHERE execution_id=$1", execution) == nil, 409, "dispatch_conflict")
	t.exec("INSERT INTO clone_executions(execution_id,operation_id,node_id,input,dispatched_epoch) VALUES($1,$2,$3,$4,$5)", execution, operation, node, jsonText(input), r.Body.N("epoch"))
	t.exec("UPDATE clone_requests SET state='dispatched',updated_at=now() WHERE id=$1", operation)
	return t.one("SELECT * FROM clone_executions WHERE execution_id=$1", execution)
}

// cloneResult commits an execution fact. With a receipt it is the takeover of a Node event and the
// only path that later justifies an Ack; without one it is a queried result. Identical facts are
// idempotent, differing facts or receipts conflict and leave the original untouched.
func cloneResult(t *transaction, r *ControlRequest, withReceipt bool) Object {
	operation, execution := r.Body.S("operationId"), r.Body.S("executionId")
	result := r.Body.O("result")
	e := t.one("SELECT * FROM clone_executions WHERE execution_id=$1 AND operation_id=$2", execution, operation)
	require(e != nil, 404, "not_found")
	outcome := result.S("outcome")
	require((outcome == "clone_ready" || outcome == "clone_failed") && result.O("node").S("nodeId") == e.S("nodeId"), 409, "result_conflict")
	if previous := e.O("result"); len(previous) > 0 {
		require(jsonText(previous) == jsonText(result), 409, "result_conflict")
	} else {
		state := "succeeded"
		if outcome == "clone_failed" {
			state = "failed"
		}
		t.exec("UPDATE clone_executions SET result=$2,updated_at=now() WHERE execution_id=$1", execution, jsonText(result))
		t.exec("UPDATE clone_requests SET state=$2,updated_at=now() WHERE id=$1", operation, state)
	}
	if withReceipt {
		sequence, event := r.Body.N("sequence"), r.Body.S("event")
		require(sequence >= 0 && event != "", 400, "invalid_receipt")
		if old := t.one("SELECT * FROM clone_event_receipts WHERE execution_id=$1 AND sequence=$2", execution, sequence); old != nil {
			require(old.S("event") == event, 409, "receipt_conflict")
		} else {
			t.exec("INSERT INTO clone_event_receipts(execution_id,sequence,event) VALUES($1,$2,$3)", execution, sequence, event)
		}
	}
	return t.one("SELECT * FROM clone_executions WHERE execution_id=$1", execution)
}

// isCloneAction reports whether a control action belongs to the execution registry.
func isCloneAction(action string) bool { return strings.HasPrefix(action, "clone_") }
