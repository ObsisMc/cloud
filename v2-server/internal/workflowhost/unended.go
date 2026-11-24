package workflowhost

import (
	"errors"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// endRetryLimit caps the wait between retries of an End the execution service did not confirm.
const endRetryLimit = 30 * time.Second

// linger keeps the slot of a session whose End was not confirmed after its owner stopped tracking
// it: Node may still hold the session on the run's checkout, and a start admitted in its place
// could be refused with checkout_session_limit. It reports whether the session was added, so the
// caller starts its retry (see retryEnd) once it released mu. Callers hold mu.
func (st *runState) linger(session string) bool {
	if st.unended == nil {
		st.unended = map[string]bool{}
	}
	if st.unended[session] {
		return false
	}
	st.unended[session] = true
	return true
}

// retryEnd ends a lingering session whose End was tried and not confirmed: again after endRetry,
// then backing off up to endRetryLimit, until the execution service confirms it or the host shuts
// down. Only then does the session give its slot back; the run's state is kept until then (see
// forgetFinished).
//
// Retrying is only worth it while the run can still start sessions. Once it is cancelled or no
// longer running, the slot has no use, and every End of a session whose stop was not proven lets
// the execution service rebuild the project's Node, which ends the project's other sessions too.
// So the session gives its slot back without another End and is left to whatever ends a session
// in the project next.
func (h *Host) retryEnd(st *runState, runID, session string) {
	stop := func() bool {
		if !h.runEnded(st, runID) {
			return false
		}
		h.releaseLingering(st, runID, session)
		return true
	}
	h.endUntilConfirmed(st, session, stop, func() { h.releaseLingering(st, runID, session) })
}

// endLingering ends a lingering session nobody tried to end yet, and retries an unconfirmed End
// like retryEnd.
func (h *Host) endLingering(st *runState, runID, session string) {
	if endConfirmed(h.end(st.scope, session)) {
		h.releaseLingering(st, runID, session)
		return
	}
	h.retryEnd(st, runID, session)
}

// releaseLingering gives a lingering session's slot back and forgets the run's state when nothing
// else of it is left.
func (h *Host) releaseLingering(st *runState, runID, session string) {
	st.mu.Lock()
	delete(st.unended, session)
	st.slotsMayHaveChanged()
	st.mu.Unlock()
	h.forgetFinished(runID, st)
}

// runEnded reports whether st's run starts no further session: it is being cancelled, or it failed,
// was cancelled or finished. A run whose state cannot be read counts as still running.
func (h *Host) runEnded(st *runState, runID string) bool {
	st.mu.Lock()
	cancelling := st.cancelling
	st.mu.Unlock()
	return cancelling || h.runStopped(h.ctx, runID)
}

// endUntilConfirmed retries End in the background on retryEnd's schedule until it is confirmed,
// then runs confirmed. Before every retry, and again after its wait, it asks stop and leaves
// without calling confirmed when stop reports true: someone else confirmed the End meanwhile, or
// the retry is not wanted any more. It also leaves when the host shuts down.
func (h *Host) endUntilConfirmed(st *runState, session string, stop func() bool, confirmed func()) {
	h.workers.Add(1)
	go func() {
		defer h.workers.Done()
		delay := h.endRetry
		timer := time.NewTimer(delay)
		defer timer.Stop()
		for {
			if stop() {
				return
			}
			select {
			case <-h.ctx.Done():
				return
			case <-timer.C:
			}
			// The timer and the shutdown may be ready together; an End now would hold Close up
			// for a whole stop timeout.
			if h.ctx.Err() != nil || stop() {
				return
			}
			if endConfirmed(h.end(st.scope, session)) {
				break
			}
			delay = min(delay*2, endRetryLimit)
			timer.Reset(delay)
		}
		confirmed()
	}()
}

// endConfirmed reports whether an End's outcome proves Node no longer holds the session: it
// succeeded, or the session is unknown or already ended.
func endConfirmed(err error) bool {
	return err == nil || errors.Is(err, execution.ErrNotFound) || errors.Is(err, execution.ErrSessionEnded)
}

// endInteractive ends n's session and, once End is confirmed, gives its slot back. Until then the
// node keeps its slot and, while the run runs, End is retried in the background: a stopped turn
// may leave nothing to complete, so no completion would end it again. Whoever stops tracking the
// node meanwhile hands the session to linger instead (see dropInteractive).
func (h *Host) endInteractive(st *runState, n *interactiveNode) {
	h.tools.Revoke(n.d.NodeRunID)
	if !endConfirmed(h.end(st.scope, n.session)) {
		st.mu.Lock()
		retrying := n.endRetrying
		n.endRetrying = true
		st.mu.Unlock()
		if retrying {
			return
		}
		// The node keeps its slot when its run ended: whoever drops it hands the session to
		// retryEnd, which gives the slot back without retrying.
		stop := func() bool {
			st.mu.Lock()
			settled := n.sessionGone || st.interactive[n.d.Node.ID] != n
			st.mu.Unlock()
			return settled || h.runEnded(st, n.d.RunID)
		}
		h.endUntilConfirmed(st, n.session, stop, func() { h.sessionGone(st, n) })
		return
	}
	h.sessionGone(st, n)
}

// sessionGone records that Node no longer holds n's session and gives its slot back.
func (h *Host) sessionGone(st *runState, n *interactiveNode) {
	st.mu.Lock()
	n.sessionGone = true
	st.slotsMayHaveChanged()
	st.mu.Unlock()
}

// dropInteractive stops tracking n, if it is still tracked. A session whose End was not confirmed
// keeps its slot as a lingering session; it reports whether the caller must start its retry.
// Dropping n never frees a slot by itself: a gone session gave its slot back (and woke the
// waiters) when its End was confirmed. Callers hold mu.
func (st *runState) dropInteractive(n *interactiveNode) bool {
	if st.interactive[n.d.Node.ID] != n {
		return false
	}
	delete(st.interactive, n.d.Node.ID)
	return !n.sessionGone && st.linger(n.session)
}

// forgetInteractive is dropInteractive for a caller not holding mu, which then retries the End of a
// session still held (see retryEnd); every caller has tried to end it already.
func (h *Host) forgetInteractive(st *runState, n *interactiveNode) {
	h.tools.Revoke(n.d.NodeRunID)
	st.mu.Lock()
	retry := st.dropInteractive(n)
	st.mu.Unlock()
	if retry {
		h.retryEnd(st, n.d.RunID, n.session)
	}
}
