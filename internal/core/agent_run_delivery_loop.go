package core

import "context"

// The two background recovery passes of the delivery/release half (IssueRun D3/D5, operation D4;
// plan §5 Batch 2). Both follow the repository's established bounded-pass shape — a read-only scan
// outside the advisory lock, then one short transaction per run, with the sleep between ticks owned
// by the caller — exactly like the dispatch, session-start, idle-Thread and cancel passes. Neither
// pass makes a business decision the durable state has not already made:
//
//	GiveUpStaleDeliveriesOnce      applies D5's two give-up limits to a run the delivery path has
//	                               already left `delivering`; it decides nothing that a fresh
//	                               delivery result would not decide, it only decides it on the clock.
//	RedeclareRunWorkspaceDeletesOnce  re-declares a delete intent that the `releasing` transition
//	                               already committed. The run being `releasing` IS the decision; this
//	                               pass only guarantees the Controller has an operation to execute.
//
// Neither invents a limit. D5 fixes the give-up window and the unreachability window as Cloud
// configuration ("这两个上限是第一版默认值（Cloud 配置项）"), and the delete re-declaration has no
// limit at all: a `releasing` run must reach `done`, and D3's own durability rule ("Controller 丢信号时
// 靠周期领取兜底") is precisely that Cloud keeps a durable intent reachable until it is executed.

// agentDeliveryBatchSize bounds one scan pass so a single tick advances at most this many runs.
// Ordering is deterministic (the oldest first) so every tick drains the backlog without starving.
const agentDeliveryBatchSize = 100

// giveUpStaleDelivery releases one `delivering` run whose delivery has exceeded D5's limits, in its
// own short transaction. The re-read repeats the scan's predicate, so a run that settled its delivery
// (or was released by another tick) between the scan and this transaction is a deterministic no-op
// rather than an error.
//
// The whole decision is `deliveryGivenUp`, and it is taken inside the transaction that writes the
// release: a limit that is checked outside the write could release a run whose retry had just
// succeeded, which is exactly the "don't pre-empt a delivery that is still alive" obligation.
func (s *Store) giveUpStaleDelivery(ctx context.Context, runID string) error {
	_, err := s.transact(ctx, func(t *transaction) Object {
		o := t.one(`
			SELECT * FROM issue_runs
			WHERE id = $1 AND executor_type = 'agent' AND deleted_at IS NULL AND phase = 'delivering'`, runID)
		if o == nil {
			return Object{}
		}
		if !s.deliveryGivenUp(t, o) {
			return Object{}
		}
		// No Revision id: this path gives a delivery up, so the run's result records an explicit null
		// `revisionId` beside `deliveryState = failed` (Cloud Revision D4, IssueRun D5).
		if err := s.releaseAfterDelivery(t, o, DeliveryFailed, ""); err != nil {
			panic(databaseFailure{err})
		}
		return Object{}
	})
	return err
}

// scanStaleDeliveringRuns returns the bounded, deterministically ordered ids of `delivering` runs.
// It deliberately does not evaluate D5's limits: the limits are judged on database time against
// configured durations, and repeating them per run in SQL would put the policy in two places. The
// scan only narrows to the phase; giveUpStaleDelivery decides. Runs already past a limit are the
// oldest, so ordering by the delivery's own start keeps the pass from starving them.
func (s *Store) scanStaleDeliveringRuns(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.Pool.QueryContext(ctx, `
		SELECT ir.id FROM issue_runs ir
		WHERE ir.executor_type = 'agent'
		  AND ir.deleted_at IS NULL
		  AND ir.phase = 'delivering'
		ORDER BY (SELECT min(w.created_at) FROM execution_work w
		          WHERE w.run_id = ir.id AND w.kind = 'deliver_revision'), ir.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GiveUpStaleDeliveriesOnce is one bounded pass of D5's give-up policy: find the runs still
// `delivering` (read-only) and release each one whose delivery has exhausted the configured windows,
// in its own short transaction. Per-run failures are deliberately not surfaced — releaseAfterDelivery
// rolls its transaction back and leaves the run `delivering` for a later tick, and the loop must not
// flood logs because one run is transiently stuck. Only a scan-level failure cancels the pass.
//
// A deployment that configured neither limit is a no-op, not an immediate give-up: with both windows
// unset nothing is ever stale (deliveryGivenUp fails closed), so the scan would only burn a query.
func (s *Store) GiveUpStaleDeliveriesOnce(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.DeliveryGiveUpAfter <= 0 && s.DeliveryUnreachableAfter <= 0 {
		return nil
	}
	ids, err := s.scanStaleDeliveringRuns(ctx, agentDeliveryBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = s.giveUpStaleDelivery(ctx, id)
	}
	return nil
}

// redeclareRunWorkspaceDelete re-declares the run Workspace's delete for one `releasing` run whose
// delete intent has no operation in flight, in its own short transaction. The re-read repeats the
// scan's predicate; the seam re-reads the run and Workspace authoritatively again and is idempotent
// on an operation that is still in flight, so a second tick cannot produce a second operation.
//
// A run that is no longer `releasing` is a no-op, and a declaration failure (a busy Project, a
// missing Workspace) rolls the transaction back and leaves the run `releasing` — the intent is not
// lost, it is simply not yet executable, and the next tick retries it.
func (s *Store) redeclareRunWorkspaceDelete(ctx context.Context, runID string) error {
	_, err := s.transact(ctx, func(t *transaction) Object {
		o := t.one(`
			SELECT * FROM issue_runs
			WHERE id = $1 AND executor_type = 'agent' AND deleted_at IS NULL AND phase = 'releasing'`, runID)
		if o == nil {
			return Object{}
		}
		if err := s.declareDelete(t, o); err != nil {
			panic(databaseFailure{err})
		}
		return Object{}
	})
	return err
}

// scanUndeclaredReleasingRuns returns the bounded, deterministically ordered ids of `releasing` runs
// whose run Workspace has no unfinished delete_workspace operation. The phase and the missing
// operation are both read here; the authoritative decision is repeated inside each per-run
// transaction and again inside the seam, so a stale id is harmless.
func (s *Store) scanUndeclaredReleasingRuns(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.Pool.QueryContext(ctx, `
		SELECT ir.id FROM issue_runs ir
		WHERE ir.executor_type = 'agent'
		  AND ir.deleted_at IS NULL
		  AND ir.phase = 'releasing'
		  AND NOT EXISTS (
		    SELECT 1 FROM operations o
		    WHERE o.workspace_id = ir.workspace_id
		      AND o.kind = 'delete_workspace'
		      AND o.state IN ('queued','running','retry_wait','blocked'))
		ORDER BY ir.updated_at, ir.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RedeclareRunWorkspaceDeletesOnce is one bounded pass of the release reconciliation: find the
// `releasing` runs whose Workspace delete has no operation in flight (read-only) and re-declare each
// in its own short transaction. Per-run failures are not surfaced, for the same reason as every other
// pass: the transaction rolled back, the run is unchanged, and the next tick retries it. Only a
// scan-level failure cancels the pass.
//
// It exists because a delete operation can end without settling the run: a Node that refuses to
// quiesce fails the operation and restores the Workspace (restoreAdmission), which is operation D4's
// documented "quiesce 失败并按原规则重试" — and for a run Workspace the retrying party is Cloud, since
// the run Workspace has no public API and no user to retry it.
func (s *Store) RedeclareRunWorkspaceDeletesOnce(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	ids, err := s.scanUndeclaredReleasingRuns(ctx, agentDeliveryBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = s.redeclareRunWorkspaceDelete(ctx, id)
	}
	return nil
}
