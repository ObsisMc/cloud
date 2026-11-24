//go:build linux

package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
)

// Host tools reach an Agent as host MCP servers in its session's start and as a block after each
// prompt (execution.HostTools). Node records that block in its own history, and the Agent's tool
// calls carry the token it names, so everything published from that history is cleared of both
// here, before an event, a result or a handoff can hold it.

// hostRedacted stands in for a credential in a published record.
const hostRedacted = "[redacted]"

// takesHostTools reports whether this Node incarnation takes host MCP servers.
func (p *projectRuntime) takesHostTools() bool {
	capable, ok := p.ipc().(interface{ Supports(string) bool })
	return ok && capable.Supports("agent_session") && capable.Supports("session_host_mcp_servers")
}

// CheckHostTools starts the project's Node if it is not running and reports whether it takes host
// MCP servers.
func (d *Driver) CheckHostTools(ctx context.Context, scope execution.Scope, projectID string) error {
	config, err := d.opts.ResolveProject(ctx, scope, projectID)
	if err != nil {
		return execution.ErrUnavailable
	}
	p, err := d.project(ctx, projectID, config)
	if err != nil {
		return err
	}
	if !p.takesHostTools() {
		return execution.ErrHostMCPUnavailable
	}
	return nil
}

// instruction issues the turn's host instruction, once while the turn is active.
func (a *agentSession) instruction(turn execution.Turn) execution.HostInstruction {
	a.mu.Lock()
	call := a.active
	if call != nil && call.id == turn.Ref.TurnID && call.instruction != nil {
		defer a.mu.Unlock()
		return *call.instruction
	}
	a.mu.Unlock()
	issued := turn.Host.Instruct()
	if call != nil && call.id == turn.Ref.TurnID {
		a.mu.Lock()
		call.instruction = &issued
		a.mu.Unlock()
	}
	return issued
}

// conceal remembers a block only the Agent gets and credentials it holds.
func (a *agentSession) conceal(block string, secrets ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if block != "" {
		if a.hostBlocks == nil {
			a.hostBlocks = map[string]bool{}
		}
		a.hostBlocks[block] = true
	}
	for _, s := range secrets {
		if s != "" {
			a.hostSecrets = append(a.hostSecrets, s)
		}
	}
}

// censor replaces every credential in text.
func (a *agentSession) censor(text string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.hostSecrets {
		text = strings.ReplaceAll(text, s, hostRedacted)
	}
	return text
}

// censorRecord replaces every credential in a JSON document, leaving the given one as it is.
func (a *agentSession) censorRecord(data []byte) []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.censorLocked(data)
}

// censorLocked is censorRecord for a caller holding mu. Credentials are plain hex, so they read the
// same inside JSON text.
func (a *agentSession) censorLocked(data []byte) []byte {
	for _, s := range a.hostSecrets {
		if bytes.Contains(data, []byte(s)) {
			data = bytes.ReplaceAll(data, []byte(s), []byte(hostRedacted))
		}
	}
	return data
}

// The places a Node record keeps credentials of host tools in: the instruction block and the tool
// call's argument, also where a record quotes them as JSON text. Every match stays inside one JSON
// string. unknownRuns finds what else they may be quoted in (an answer, a URL), as any run of at
// least as many hex digits; it matches escapes only to step over them.
var (
	unknownBlock = regexp.MustCompile(`((?:<|\\+u003c)workflow_completion(?:>|\\+u003e))[^"]*?((?:<|\\+u003c)/workflow_completion(?:>|\\+u003e))`)
	unknownToken = regexp.MustCompile(`(attemptToken\\*"\s*:\s*\\*")[^"\\]*`)
	unknownRuns  = regexp.MustCompile(`\\u[0-9a-fA-F]{4}|\\.|[0-9a-f]{32,}`)
)

// preparationResult reports whether message is the result of a clone or a plugin installation. No
// Agent runs in those executions, so no host tool ever reached one, and their commits and
// plugin versions are what the journal keeps them for. Any other message of an unknown execution may be an Agent
// session's.
func preparationResult(message node.Message) bool {
	return message.Type == "clone_result" || message.Type == "plugins_result"
}

// censorUnknown replaces the credentials host tools leave in a JSON document nobody remembers them
// for. It also takes identifiers and digests of that shape: the journal is a diagnostic, and this one
// of an Agent session that is over.
func censorUnknown(data []byte) []byte {
	out := unknownBlock.ReplaceAll(data, []byte("${1}"+hostRedacted+"${2}"))
	out = unknownToken.ReplaceAll(out, []byte("${1}"+hostRedacted))
	out = unknownRuns.ReplaceAllFunc(out, func(match []byte) []byte {
		if match[0] == '\\' {
			return match
		}
		return []byte(hostRedacted)
	})
	if len(data) != 0 && !json.Valid(out) {
		return []byte(`"` + hostRedacted + `"`)
	}
	return out
}

// withhold removes the host blocks from a snapshot and every credential from the rest.
func (a *agentSession) withhold(snapshot *execution.HistorySnapshot) *execution.HistorySnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.hostSecrets) == 0 {
		return snapshot
	}
	shown := &execution.HistorySnapshot{Incomplete: snapshot.Incomplete}
	for _, record := range snapshot.Records {
		if a.hostBlocks[userBlock(record.Data)] {
			continue
		}
		record.Data = a.censorLocked(record.Data)
		shown.Records = append(shown.Records, record)
	}
	return shown
}

// userBlock is the text of a Node history record holding one block of a user turn, or empty.
func userBlock(data []byte) string {
	var line struct {
		Type   string `json:"type"`
		Update struct {
			Kind    string `json:"sessionUpdate"`
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	}
	if json.Unmarshal(data, &line) != nil || line.Type != "update" || line.Update.Kind != "user_message_chunk" {
		return ""
	}
	return line.Update.Content.Text
}
