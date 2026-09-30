package simulator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/wanglongan587/cloud/internal/controlpb"
)

// deliverRevision resolves the actual clone and sealed echo history. Its disk journal contains
// terminal evidence only; fresh grants are obtained on every upload/restart and never serialized.
func (c *Controller) deliverRevision(ctx context.Context, record *controlpb.ExecutionRecord, journal *agentJournal) (*controlpb.ExecutionResult, error) {
	if len(journal.Result) > 0 {
		result := &controlpb.ExecutionResult{}
		if err := protojson.Unmarshal(journal.Result, result); err != nil {
			return nil, fmt.Errorf("decode durable delivery evidence: %w", err)
		}
		return result, nil
	}
	spec := record.GetInput().GetDeliverRevision()
	result := &controlpb.ExecutionResult{Node: &controlpb.NodeIdentity{NodeId: record.GetNodeId(), NodeIncarnationId: "echo-fixture"}}
	failure := func(reason controlpb.RevisionFailureReason) (*controlpb.ExecutionResult, error) {
		result.Outcome = &controlpb.ExecutionResult_RevisionFailed{RevisionFailed: &controlpb.RevisionFailed{Reason: reason}}
		return result, c.saveRevisionResult(record.GetExecutionId(), journal, result)
	}
	session, err := c.Executions.GetDispatch(ctx, &controlpb.GetDispatchRequest{ExecutionId: spec.GetSessionExecutionId()})
	if err != nil {
		return nil, err
	}
	sealed, err := c.AgentNode.load(session.GetRecord())
	if err != nil || sealed.Ended == controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_UNSPECIFIED {
		return failure(controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_SESSION_NOT_SETTLED)
	}
	clone, err := c.Executions.GetDispatch(ctx, &controlpb.GetDispatchRequest{ExecutionId: spec.GetCheckoutExecutionId()})
	if err != nil {
		return nil, err
	}
	checkout := clone.GetRecord().GetResult().GetCloneReady()
	if checkout == nil || checkout.GetCommit() != spec.GetBaseCommit() || checkout.GetPath() == "" {
		return failure(controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_CHECKOUT_UNAVAILABLE)
	}
	result.Node = clone.GetRecord().GetResult().GetNode()
	dir, err := os.MkdirTemp(c.AgentNode.root, "revision-")
	if err != nil {
		return nil, fmt.Errorf("create delivery scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	final, err := snapshotRevision(ctx, checkout.GetPath(), dir, spec.GetRevisionRef(), session.GetRecord().GetInput().GetAgentSession().GetGitIdentity())
	if err != nil {
		return failure(controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_SNAPSHOT_FAILED)
	}
	var history bytes.Buffer
	for _, event := range sealed.Events {
		history.WriteString(event.GetRecord())
		history.WriteByte('\n')
	}
	files := map[string][]byte{spec.GetHistoryKey(): history.Bytes()}
	historyObject := storedBytes(spec.GetHistoryKey(), history.Bytes())
	if final == spec.GetBaseCommit() {
		result.Outcome = &controlpb.ExecutionResult_RevisionUnchanged{RevisionUnchanged: &controlpb.RevisionUnchanged{BaseCommit: spec.GetBaseCommit(), FinalCommit: final, RevisionRef: spec.GetRevisionRef(), History: historyObject}}
	} else {
		bundlePath := filepath.Join(dir, "revision.bundle")
		if _, err = git(ctx, "-C", checkout.GetPath(), "bundle", "create", bundlePath, spec.GetRevisionRef(), "^"+spec.GetBaseCommit()); err != nil {
			return failure(controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_BUNDLE_FAILED)
		}
		if _, err = git(ctx, "-C", checkout.GetPath(), "bundle", "verify", bundlePath); err != nil {
			return failure(controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_BUNDLE_FAILED)
		}
		bundle, readErr := os.ReadFile(bundlePath)
		if readErr != nil {
			return nil, fmt.Errorf("read delivery bundle: %w", readErr)
		}
		files[spec.GetBundleKey()] = bundle
		result.Outcome = &controlpb.ExecutionResult_RevisionDelivered{RevisionDelivered: &controlpb.RevisionDelivered{BaseCommit: spec.GetBaseCommit(), FinalCommit: final, RevisionRef: spec.GetRevisionRef(), Bundle: storedBytes(spec.GetBundleKey(), bundle), History: historyObject}}
	}
	for key, data := range files {
		if err = c.uploadRevisionObject(ctx, record.GetExecutionId(), key, data); err != nil {
			return failure(controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED)
		}
	}
	return result, c.saveRevisionResult(record.GetExecutionId(), journal, result)
}

func (c *Controller) saveRevisionResult(execution string, journal *agentJournal, result *controlpb.ExecutionResult) error {
	data, err := protojson.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode durable delivery evidence: %w", err)
	}
	journal.Result = data
	return c.AgentNode.save(execution, journal)
}

func storedBytes(key string, data []byte) *controlpb.StoredObject {
	sum := sha256.Sum256(data)
	return &controlpb.StoredObject{Key: key, Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:])}
}

// snapshotRevision uses a scratch index, never the user's index, branch or commit hooks.
func snapshotRevision(ctx context.Context, checkout, scratch, ref string, identity *controlpb.GitIdentity) (string, error) {
	if !strings.HasPrefix(ref, "refs/ora/revisions/") {
		return "", fmt.Errorf("invalid revision ref")
	}
	if _, err := git(ctx, "-C", checkout, "check-ref-format", ref); err != nil {
		return "", err
	}
	head, err := git(ctx, "-C", checkout, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	status, err := git(ctx, "-C", checkout, "status", "--porcelain=v2", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	final := head
	if status != "" {
		run := func(args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "git", append([]string{"-C", checkout, "-c", "core.hooksPath="}, args...)...) // #nosec G204 -- simulator-only Git, clone-owned path and fixed plumbing arguments below.
			cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(scratch, "index"), "GIT_AUTHOR_NAME="+identity.GetName(), "GIT_AUTHOR_EMAIL="+identity.GetEmail(), "GIT_COMMITTER_NAME=Ora", "GIT_COMMITTER_EMAIL=revision@ora.invalid")
			output, err := cmd.Output()
			return strings.TrimSpace(string(output)), err
		}
		if _, err = run("read-tree", "HEAD"); err != nil {
			return "", err
		}
		if _, err = run("add", "-A"); err != nil {
			return "", err
		}
		tree, err := run("write-tree")
		if err != nil {
			return "", err
		}
		final, err = run("commit-tree", tree, "-p", head, "-m", "Ora internal snapshot of uncommitted run changes")
		if err != nil {
			return "", err
		}
	}
	_, err = git(ctx, "-C", checkout, "update-ref", ref, final)
	return final, err
}

func (c *Controller) uploadRevisionObject(ctx context.Context, execution, key string, data []byte) error {
	sum := sha256.Sum256(data)
	for range 3 {
		response, err := c.AgentRuns.GrantRevisionUpload(ctx, &controlpb.GrantRevisionUploadRequest{Epoch: c.Epoch, ExecutionId: execution, Checksums: map[string]string{key: hex.EncodeToString(sum[:])}})
		if err != nil {
			return fmt.Errorf("delivery grant unavailable")
		}
		for _, grant := range response.GetGrants() {
			if grant.GetObjectKey() != key || !grant.GetExpiresAt().AsTime().After(time.Now()) {
				continue
			}
			req, err := http.NewRequestWithContext(ctx, grant.GetMethod(), grant.GetUrl(), bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("delivery upload request invalid")
			}
			for name, value := range grant.GetHeaders() {
				req.Header.Set(name, value)
			}
			req.Header.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
			client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("delivery upload failed")
}
