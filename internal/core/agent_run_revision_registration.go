package core

import "fmt"

// Registering one Revision, inside the delivery execution's takeover transaction
// (Cloud Revision D4 "Revision 行的身份、唯一性与登记幂等"; IssueRun D5).
//
// The row is written by the control plane, after `node_executions.result` and before the
// DeliverySettled hook, in the one transaction that also writes the receipt and moves the run. The
// decision to write it was made outside the transaction (D4 steps 1–2, verifyDeliveryObjects); what
// is enforced here is the part that must not be racy: the run must still be `delivering`, and a
// second row for the same run must be either the same payload or a refusal.

// revisionIdentity is the Revision's content identity in the one spelling both the incoming payload
// and the stored row are reduced to before they are compared (D4: two spellings of the same outcome
// must compare equal, and any difference is an invariant conflict).
//
// The bundle fields are empty together when the checkout did not change; the history fields are
// always present, because an unchanged checkout still saves the session history (invariant 5).
type revisionIdentity struct {
	tenantID      string
	runID         string
	workspaceID   string
	projectID     string
	repositoryURL string
	baseCommit    string
	finalCommit   string
	revisionRef   string
	bundleKey     string
	bundleSize    int64
	bundleSHA256  string
	historyKey    string
	historySize   int64
	historySHA256 string
}

// diff names the first field on which two identities disagree, or "" when they are the same identity.
// Naming the field is what makes an invariant conflict actionable: the takeover rolls back either
// way, but the log has to say whether the commits, the ref or an object changed.
//
// The identities are read through pointers because neither is mutated or retained: a fourteen-string
// struct copied twice per comparison would be the only cost of the value form.
func (r *revisionIdentity) diff(o *revisionIdentity) string {
	switch {
	case r.tenantID != o.tenantID:
		return "tenant_id"
	case r.runID != o.runID:
		return "run_id"
	case r.workspaceID != o.workspaceID:
		return "workspace_id"
	case r.projectID != o.projectID:
		return "project_id"
	case r.repositoryURL != o.repositoryURL:
		return "repository_url"
	case r.baseCommit != o.baseCommit:
		return "base_commit"
	case r.finalCommit != o.finalCommit:
		return "final_commit"
	case r.revisionRef != o.revisionRef:
		return "revision_ref"
	case r.bundleKey != o.bundleKey:
		return "bundle_key"
	case r.bundleSize != o.bundleSize:
		return "bundle_size"
	case r.bundleSHA256 != o.bundleSHA256:
		return "bundle_sha256"
	case r.historyKey != o.historyKey:
		return "history_key"
	case r.historySize != o.historySize:
		return "history_size"
	case r.historySHA256 != o.historySHA256:
		return "history_sha256"
	default:
		return ""
	}
}

// storedIdentity reduces a `revisions` row to the comparable identity. An absent column and an empty
// string are the same thing for every field here — the schema's own constraints keep the bundle
// columns all-or-nothing and the two keys non-empty — so a NULL bundle reads as "no bundle" rather
// than as a zero-sized upload.
func storedIdentity(row Object) revisionIdentity {
	return revisionIdentity{
		tenantID:      row.S("tenantId"),
		runID:         row.S("runId"),
		workspaceID:   row.S("workspaceId"),
		projectID:     row.S("projectId"),
		repositoryURL: row.S("repositoryUrl"),
		baseCommit:    row.S("baseCommit"),
		finalCommit:   row.S("finalCommit"),
		revisionRef:   row.S("revisionRef"),
		bundleKey:     row.S("bundleKey"),
		bundleSize:    row.N("bundleSize"),
		bundleSHA256:  row.S("bundleSha256"),
		historyKey:    row.S("historyKey"),
		historySize:   row.N("historySize"),
		historySHA256: row.S("historySha256"),
	}
}

// declaredIdentity builds the identity a delivered or unchanged result asks Cloud to register. The
// digests are normalized to lowercase here because that is the spelling the column CHECK and every
// later comparison use (D4 compares digests, not their text).
//
// ok=false means the result cannot name a Revision at all — a missing or malformed digest, or a
// delivery that declares neither a usable bundle nor the unchanged shape. The caller reaches this
// only after the shape check has passed, so a failure here is an internal contradiction rather than
// a refusal to report to a Node.
func declaredIdentity(result, run, placement Object) (revisionIdentity, bool) {
	bundle := result.O("bundle")
	history := result.O("history")
	historySHA256, ok := revisionDigest(history.S("sha256"))
	if !ok {
		return revisionIdentity{}, false
	}
	identity := revisionIdentity{
		tenantID:      run.S("tenantId"),
		runID:         run.S("id"),
		workspaceID:   run.S("workspaceId"),
		projectID:     placement.S("projectId"),
		repositoryURL: placement.S("repositoryUrl"),
		baseCommit:    result.S("baseCommit"),
		finalCommit:   result.S("finalCommit"),
		revisionRef:   result.S("revisionRef"),
		historyKey:    history.S("key"),
		historySize:   history.N("size"),
		historySHA256: historySHA256,
	}
	if len(bundle) > 0 {
		bundleSHA256, ok := revisionDigest(bundle.S("sha256"))
		if !ok {
			return revisionIdentity{}, false
		}
		identity.bundleKey, identity.bundleSize, identity.bundleSHA256 = bundle.S("key"), bundle.N("size"), bundleSHA256
	}
	return identity, true
}

// registerRevision writes the Revision row for one verified delivery result, or returns the row a
// previous attempt already registered for the same run (Cloud Revision D4).
//
// Three outcomes, and the caller must distinguish all three:
//
//	("", false, nil)   the run has left `delivering`, so invariant 9 applies: this result decides
//	                   nothing and registers nothing, whatever it carries.
//	(id, true, nil)    the run has its Revision: freshly inserted, or an identical row an earlier
//	                   attempt already registered — the replay is idempotent and answers the same id.
//	("", false, err)   an invariant failure: the run or its Workspace is gone, the payload cannot
//	                   name a Revision, or a row exists for this run with a DIFFERENT payload.
//
// The last case is the one the approved decision spells out as "整条事务回滚": the takeover panics
// databaseFailure, so the receipt, the durable result, the Revision and the run's transition roll
// back together and the Node replays the event rather than Cloud keeping a half-believed fact.
func (s *Store) registerRevision(t *transaction, runID string, result Object) (id string, registered bool, err error) {
	run := t.one("SELECT * FROM issue_runs WHERE id=$1 AND deleted_at IS NULL", runID)
	if run == nil {
		return "", false, fmt.Errorf("register revision: issue_run %s not found", runID)
	}
	if run.S("phase") != "delivering" {
		// Invariant 9, enforced at the write rather than by every caller: a result that arrives after
		// the delivery was settled cannot create a Revision, because the phase it would have to be
		// registered in is the phase that guarantees the run has not yet decided.
		return "", false, nil
	}
	placement := t.one(`
		SELECT w.project_id, p.repository_url FROM workspaces w
		JOIN projects p ON p.id = w.project_id
		WHERE w.id=$1 AND w.issue_run_id=$2`, run.S("workspaceId"), runID)
	if placement == nil {
		return "", false, fmt.Errorf("register revision: run %s has no live run workspace %s", runID, run.S("workspaceId"))
	}
	identity, ok := declaredIdentity(result, run, placement)
	if !ok {
		return "", false, fmt.Errorf("register revision: run %s carries a result that cannot name a Revision", runID)
	}
	// ON CONFLICT DO NOTHING + read-back, rather than a pre-check: the advisory lock already
	// serializes this transaction against every other writer, and the unique index is the authority
	// the decision rests on (D4). Nothing is overwritten here, ever.
	t.exec(`
		INSERT INTO revisions(id, tenant_id, run_id, workspace_id, project_id, repository_url,
		                      base_commit, final_commit, revision_ref,
		                      bundle_key, bundle_size, bundle_sha256,
		                      history_key, history_size, history_sha256)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (run_id) DO NOTHING`,
		newID(), identity.tenantID, identity.runID, identity.workspaceID, identity.projectID, identity.repositoryURL,
		identity.baseCommit, identity.finalCommit, identity.revisionRef,
		nullableText(identity.bundleKey), nullableSize(identity.bundleKey, identity.bundleSize), nullableText(identity.bundleSHA256),
		identity.historyKey, identity.historySize, identity.historySHA256)

	row := t.one("SELECT * FROM revisions WHERE run_id=$1", runID)
	if row == nil {
		// The insert was a no-op above and no row exists now: the only way both can be true is an
		// external writer removing the row mid-transaction, which the advisory lock forbids.
		return "", false, fmt.Errorf("register revision: run %s has no Revision row after registration", runID)
	}
	stored := storedIdentity(row)
	if field := identity.diff(&stored); field != "" {
		return "", false, fmt.Errorf("register revision: run %s already has a Revision whose %s differs from the delivered result", runID, field)
	}
	return row.S("id"), true, nil
}

// nullableText turns the empty string into SQL NULL, the spelling the bundle columns use when the
// checkout did not change (the schema's all-or-nothing constraint requires exactly this).
func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableSize pairs with nullableText: a size is only ever written when its key is, so the columns
// cannot drift into "a size without an object".
func nullableSize(key string, size int64) any {
	if key == "" {
		return nil
	}
	return size
}
