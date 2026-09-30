package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/wanglongan587/cloud/internal/controlpb"
)

type revisionGrantFixture struct {
	controlpb.AgentRunServiceClient
	url    string
	cancel context.CancelFunc
}

// Provide ephemeral grants without making the upload fixture depend on the business projection.
func (c *revisionGrantFixture) GrantRevisionUpload(_ context.Context, _ *controlpb.GrantRevisionUploadRequest, _ ...grpc.CallOption) (*controlpb.GrantRevisionUploadResponse, error) {
	if c.cancel != nil {
		c.cancel()
	}
	return &controlpb.GrantRevisionUploadResponse{Grants: []*controlpb.UploadGrant{{ObjectKey: "frozen/history", Url: c.url, Method: "PUT", Headers: map[string]string{"if-none-match": "*"}, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}}, nil
}

// Historical pending journals used null; reopening them must not attempt to decode a terminal fact.
func TestAgentJournalWithoutTerminalResultRemainsPending(t *testing.T) {
	node, err := NewAgentNode(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record := &controlpb.ExecutionRecord{ExecutionId: "pending-delivery", Input: &controlpb.ExecutionInput{}}
	journal, err := node.load(record)
	if err != nil {
		t.Fatal(err)
	}
	journal.Result = json.RawMessage("null")
	if err := node.save(record.ExecutionId, journal); err != nil {
		t.Fatal(err)
	}
	recovered, err := node.load(record)
	if err != nil || len(recovered.Result) != 0 {
		t.Fatal("pending null was recovered as a terminal result", err)
	}
}

// A lost PUT response retries the frozen key; 412 leaves the declaration for Cloud's real HEAD.
func TestRevisionUploadReconcilesAnAmbiguousCreatedObject(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "*" {
			t.Error("uploader omitted the signed creation condition")
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusPreconditionFailed)
	}))
	t.Cleanup(server.Close)
	c := &Controller{AgentRuns: &revisionGrantFixture{url: server.URL}}
	if err := c.uploadRevisionObject(t.Context(), "delivery", "frozen/history", []byte("sealed history")); err != nil {
		t.Fatal("existing object was converted to an upload failure", err)
	}
	if attempts.Load() != 2 {
		t.Fatal("ambiguous upload was not reconciled by the same frozen key")
	}
}

// Shutdown is a Controller lifetime event, not a durable Node terminal fact.
func TestRevisionUploadPreservesControllerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	c := &Controller{AgentRuns: &revisionGrantFixture{url: "http://unused.invalid", cancel: cancel}}
	if err := c.uploadRevisionObject(ctx, "delivery", "frozen/history", []byte("sealed history")); !errors.Is(err, context.Canceled) {
		t.Fatal("upload discarded the cancellation cause")
	}
}
