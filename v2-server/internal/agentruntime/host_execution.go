//go:build linux

package agentruntime

import (
	"context"
	"log/slog"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// A session given host tools has its Node execution recorded (Options.RecordHostExecution) before
// its start is sent, and settled once with why it ended. The first reason decided stands: what made
// this driver end the session, what Node reported, or the loss of its Node. A server that shuts down
// settles nothing: the next start settles what is still running, as it does after a crash.

const (
	hostExecutionWrite        = 10 * time.Second
	defaultHostExecutionRetry = 5 * time.Second
)

// recordHost records the execution. Its failure means the start must not be sent. The row may exist
// all the same, so from here on the execution counts as recorded.
func (a *agentSession) recordHost() error {
	record := a.project.driver.opts.RecordHostExecution
	if record == nil || a.project.driver.opts.EndHostExecution == nil {
		return execution.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(a.project.ctx, hostExecutionWrite)
	defer cancel()
	err := record(ctx, a.execution, a.frozen.ID)
	a.mu.Lock()
	a.hostRecorded = true
	decided := a.hostReason != "" // the Node may have been lost while this wrote
	a.mu.Unlock()
	if decided {
		a.settleHost("")
	}
	return err
}

// decideHost keeps reason as why the execution ended, unless one was decided before.
func (a *agentSession) decideHost(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.decideHostLocked(reason)
}

func (a *agentSession) decideHostLocked(reason string) {
	if a.hostReason == "" {
		a.hostReason = reason
	}
}

// settleHost writes that the execution ended, with reason unless one was decided before. The write
// happens before the caller goes on, so nothing that learns of the end from it finds the execution
// running; one that fails is repeated in the background.
func (a *agentSession) settleHost(reason string) {
	a.mu.Lock()
	a.decideHostLocked(reason)
	if !a.hostRecorded || a.hostSettled || a.project.driver.closing.Load() {
		a.mu.Unlock()
		return
	}
	a.hostSettled = true
	reason = a.hostReason
	a.mu.Unlock()
	if a.endHost(reason) == nil {
		return
	}
	retry := a.project.driver.hostExecutionRetry
	if retry <= 0 {
		retry = defaultHostExecutionRetry
	}
	go func() {
		for {
			time.Sleep(retry)
			if a.project.driver.closing.Load() || a.endHost(reason) == nil {
				return
			}
		}
	}()
}

func (a *agentSession) endHost(reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), hostExecutionWrite)
	defer cancel()
	err := a.project.driver.opts.EndHostExecution(ctx, a.execution, reason)
	if err != nil {
		slog.Warn("record the end of an Agent execution", "execution", a.execution, "reason", reason, "error", err)
	}
	return err
}
