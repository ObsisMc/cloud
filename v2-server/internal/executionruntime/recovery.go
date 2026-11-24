package executionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// A recovered held session's Agent was lost with the previous process (see
// store.RecoverExecutions). Its next turn starts a new Agent on the same checkout and hands it the
// recorded conversation, as desktop's rebuild_provider_session does when session/load is
// unavailable: the pinned Node cannot load a provider session, and it removes the Agent's own
// session store with the session. The new Agent learns the conversation only from the handoff,
// which names tool calls by title and outcome: no output, and an input only as far as a title
// shows it (OpenCode titles a shell call with its full command, as the page history shows). Its
// first turn has no prompt cache.

// maxTurnText is the pinned Node's limit on the text of one user turn, all blocks together
// (node-protocol MAX_USER_TURN_TEXT_BYTES). The handoff and the prompt share it.
const maxTurnText = 64 * 1024

// handoffMarkers frame the handoff block, so the Agent can tell context from the user's message.
const (
	handoffOpen  = "<ora_previous_conversation>"
	handoffClose = "</ora_previous_conversation>"
)

var errWorkspaceMissing = errors.New(execution.FailureWorkspaceMissing)

// recovery prepares what the driver needs to start the session's Agent again; its checkout was
// reattached when the session was kept (see New). gone reports a checkout that no longer exists,
// which ends the session; any other error leaves it recovering, so a later turn tries again.
func (s *Service) recovery(ctx context.Context, session execution.Session, turn execution.Turn) (*execution.Recovery, bool, error) {
	at, err := s.db.ExecutionWorkspace(ctx, session.WorkspaceID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && at.CloneExecution == "") {
		return nil, true, errWorkspaceMissing
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", execution.ErrUnavailable, err)
	}
	if bound := s.clones[session.ID]; bound != "" && at.CloneExecution != bound {
		// The workspace's record moved on to another clone: Node no longer knew the one this
		// conversation ran in, which was forgotten, and a later node of the run cloned again. The
		// conversation's edits are not in the new clone, and continuing there would lose them
		// silently, so its checkout is gone.
		return nil, true, errWorkspaceMissing
	}
	if _, err := os.Stat(at.Path); errors.Is(err, fs.ErrNotExist) {
		return nil, true, errWorkspaceMissing
	} else if err != nil {
		return nil, false, fmt.Errorf("%w: %w", execution.ErrUnavailable, err)
	}
	history, err := s.db.LatestHistory(ctx, session.ID)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", execution.ErrUnavailable, err)
	}
	prompt, err := turn.Prompt.Text()
	if err != nil {
		return nil, false, execution.ErrInvalidRequest
	}
	room := maxTurnText - len(prompt)
	if turn.Host != nil {
		room -= execution.MaxHostInstruction
	}
	handoff := renderHandoff(history, room)
	if handoff == "" {
		// The prompt leaves no room for the conversation; a new Agent without it would silently
		// start over, so the turn is refused and a shorter one can try again.
		return nil, false, execution.ErrInvalidRequest
	}
	return &execution.Recovery{History: history, Handoff: handoff}, false, nil
}

// handoffTurn is one recorded turn as the handoff lists it.
type handoffTurn struct {
	user, agent strings.Builder
	tools       []string
}

// historyLine is the part of a Node history record the handoff reads.
type historyLine struct {
	Type   string `json:"type"`
	Update struct {
		Kind    string `json:"sessionUpdate"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"update"`
}

// renderHandoff lists the recorded conversation within budget bytes, like desktop's
// render_handoff: each turn's user message and answer text, and its tool calls by title and
// outcome only. Thoughts and plans are left out. When the conversation does not fit, the oldest
// turns are left out first, and the block says so; the last turn alone is cut from its start.
func renderHandoff(records []execution.HistoryRecord, budget int) string {
	sorted := append([]execution.HistoryRecord(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Sequence < sorted[j].Sequence })
	var turns []*handoffTurn
	current := func() *handoffTurn {
		if len(turns) == 0 {
			turns = append(turns, &handoffTurn{})
		}
		return turns[len(turns)-1]
	}
	closed := true
	for _, record := range sorted {
		var line historyLine
		if json.Unmarshal(record.Data, &line) != nil {
			continue
		}
		if line.Type == "turnEnded" {
			closed = true
			continue
		}
		if line.Type != "update" {
			continue
		}
		switch line.Update.Kind {
		case "user_message_chunk":
			if closed {
				turns = append(turns, &handoffTurn{})
				closed = false
			}
			current().user.WriteString(line.Update.Content.Text)
		case "agent_message_chunk":
			current().agent.WriteString(line.Update.Content.Text)
		case "tool_call":
			title, status := line.Update.Title, line.Update.Status
			if title == "" {
				title = "tool"
			}
			if status == "" {
				status = "unknown"
			}
			current().tools = append(current().tools, title+" ("+status+")")
		}
	}
	rendered := make([]string, len(turns))
	for i, turn := range turns {
		var b strings.Builder
		fmt.Fprintf(&b, "\n## Turn %d\n", i+1)
		if turn.user.Len() > 0 {
			b.WriteString("\nUser:\n" + turn.user.String() + "\n")
		}
		for _, tool := range turn.tools {
			b.WriteString("\nTool call: " + tool + "\n")
		}
		if turn.agent.Len() > 0 {
			b.WriteString("\nAgent:\n" + turn.agent.String() + "\n")
		}
		rendered[i] = b.String()
	}
	header := func(omitted int) string {
		h := handoffOpen + "\nThis conversation's earlier Agent session was lost when its server or Node restarted. The transcript below is its history, and the work it describes has already been done in this workspace: build on it instead of repeating it. Tool calls are listed by title and outcome only; their outputs are not included. Continue from where the conversation left off. The user's new message follows this block.\n"
		if len(rendered) == 0 {
			h = handoffOpen + "\nThis conversation's earlier Agent session was lost when its server or Node restarted, and it left no readable history. Work it may have done is in this workspace. The user's new message follows this block.\n"
		}
		if omitted > 0 {
			h += fmt.Sprintf("\nThe %d earliest turns are left out to fit the limit.\n", omitted)
		}
		return h
	}
	tail := "\n" + handoffClose
	for omitted := 0; omitted <= len(rendered); omitted++ {
		body := strings.Join(rendered[omitted:], "")
		h := header(omitted)
		if len(h)+len(body)+len(tail) <= budget {
			return h + body + tail
		}
		if omitted == len(rendered)-1 {
			// Only the last turn is left and it does not fit: keep its end, the latest context.
			room := budget - len(h) - len(tail) - len("\n[…]")
			if room > 0 {
				cut := len(body) - room
				for cut < len(body) && !utf8.RuneStart(body[cut]) {
					cut++
				}
				return h + "\n[…]" + body[cut:] + tail
			}
		}
	}
	// Not even the header fits beside the prompt: hand over nothing rather than break the turn.
	return ""
}
