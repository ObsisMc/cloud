package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// chatSession creates a conversation whose first turn ran in the execution session sid.
func chatSession(t *testing.T, f *executionFixture, key, sid string) string {
	t.Helper()
	ctx := context.Background()
	c, err := f.s.CreateChatConversation(ctx, store.NewChatConversation{
		TenantID: f.scope.TenantID, UserID: f.scope.UserID, CreateKey: key,
		ProjectID: "demo", ExecutorID: "fixture", Title: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	turn := executionTurn(sid, key)
	createExecution(t, f, sid, turn)
	if err := f.s.RecordChatTurn(ctx, c.ID, sid, key, turn.Ref.TurnID, time.Now()); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func TestInterruptEndedChatSessionsAfterRecovery(t *testing.T) {
	f := executionDatabase(t)
	ctx := context.Background()
	restarted := chatSession(t, f, "restarted", "s-restarted")
	stopping := chatSession(t, f, "stopping", "s-stopping")
	// The process died between recording the stop and the port confirming it.
	if err := f.s.MarkChatSessionEnding(ctx, "s-stopping", store.EndStopped); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverExecutions(ctx); err != nil {
		t.Fatal(err)
	}
	// Started by the new process, after recovery.
	live := chatSession(t, f, "live", "s-live")
	for range 2 {
		if err := f.s.InterruptEndedChatSessions(ctx); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{restarted: store.EndInterrupted, stopping: store.EndStopped}
	for id, reason := range want {
		c, err := f.s.ChatConversation(ctx, f.scope.TenantID, f.scope.UserID, id)
		if err != nil || !c.Ended() || c.Session.EndReason != reason {
			t.Fatalf("conversation %s after recovery = %+v %v, want ended as %s", id, c.Session, err, reason)
		}
	}
	if c, err := f.s.ChatConversation(ctx, f.scope.TenantID, f.scope.UserID, live); err != nil || c.Ended() || c.Session.EndReason != "" {
		t.Fatalf("a session started after recovery must stay live: %+v %v", c.Session, err)
	}
}
