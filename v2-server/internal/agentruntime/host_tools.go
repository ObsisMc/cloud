//go:build linux

package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
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
