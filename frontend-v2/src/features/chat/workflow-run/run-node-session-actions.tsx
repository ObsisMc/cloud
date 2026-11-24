import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ContractError } from '@/lib/workflows/contracts'
import { localizeContractError } from '@/lib/workflows/host/contract-error'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  Button,
  Textarea,
  toast,
} from '@/lib/workflows/host/ui'
import type { GraphWorkflowNodeStatus, WorkflowNodeInteractiveBlocker, WorkflowNodeInteractiveSession } from '@/lib/workflows/runtime'
import { useIsRunStarter } from './run-starter'
import { useRunTenant } from './run-tenant'
import { completeNode, runKeys, sendNodeTurn, stopNodeTurn, type NodeCompletion } from './server-runs'
import { workflowRunTranslations } from './translations'

/** The longest message v2-server accepts for one turn. */
const maxTurnLength = 16000

/** The parked interactive node the actions act on. */
export interface InteractiveNodeTarget {
  runId: string
  nodeId: string
  status: GraphWorkflowNodeStatus
  session?: WorkflowNodeInteractiveSession
}

const runErrorKeys: Readonly<Record<string, string>> = workflowRunTranslations['zh-CN']

/** Refusals in the run's own words: the interactive codes live in the run view's strings. */
function refusal(error: unknown, t: (key: string, options?: Record<string, unknown>) => string): string {
  if (error instanceof ContractError) {
    if (error.code === 'workflow_node_incomplete') {
      const blockers = refusedBlockers(error)
      if (blockers.length > 0) return blockers.map((b) => blockerText(b, t)).join(' ')
    }
    const key = `errors.${error.code}`
    if (key in runErrorKeys) return t(key)
  }
  return localizeContractError(error, t as never)
}

/** The blockers a 409 workflow_node_incomplete lists in params.blockers. */
function refusedBlockers(error: ContractError): WorkflowNodeInteractiveBlocker[] {
  const list = Array.isArray(error.params.blockers) ? error.params.blockers : []
  return list.flatMap((item) => {
    if (typeof item !== 'object' || item === null) return []
    const b = item as Record<string, unknown>
    if (typeof b.kind !== 'string' || b.kind === '') return []
    return [{ kind: b.kind, ...(typeof b.path === 'string' && b.path !== '' ? { path: b.path } : {}) }]
  })
}

/** Blockers that only wait for the Agent's report or the starter's word on a turn that ended normally. */
const participation: ReadonlySet<string> = new Set(['turn_unconfirmed', 'agent_signal_pending', 'agent_confirmation_pending'])

/** One blocker in words; an unknown kind falls back to its code. */
function blockerText(blocker: WorkflowNodeInteractiveBlocker, t: (key: string, options?: Record<string, unknown>) => string): string {
  const key = `workflowRun.v2.interactive.blocker.${blocker.kind}`
  return key in runErrorKeys ? t(key, { path: blocker.path ?? '' }) : blocker.kind
}

/**
 * The starter's controls for an interactive Agent node, below its session: a composer while the
 * node waits for input and its session is open, "停止本轮" while it runs a turn, and the completion
 * actions of desktop's step card (first-turn and completion signal ADRs, D5): "完成" for a turn that
 * ended normally, "确认并完成" for the workflow's own opening turn and for a step the Agent reported
 * done, "仍然完成" behind a confirmation for any other blocker: how the turn ended, a report the
 * Agent still owes, the deliverables a refused completion named. A
 * message v2 accepted and then could not deliver is reported above the composer; the view that
 * sent it, while it still has the text, offers to put it back. Everyone else reads the transcript.
 */
export function RunNodeSessionActions({ target }: { target: InteractiveNodeTarget }) {
  const { t } = useTranslation()
  const tenantId = useRunTenant()
  const starter = useIsRunStarter()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState('')
  // The last message this view sent and the number v2 gave it among the starter's messages, kept
  // so that exactly that message can be put back in the composer if v2 reports it undelivered.
  const [lastSent, setLastSent] = useState<{ text: string; number: number }>()
  const [busy, setBusy] = useState(false)
  // Deliverables are checked only when completing, so a refusal names them; it stays shown until
  // the node settles another turn.
  const [refused, setRefused] = useState<{ turns: number; blockers: WorkflowNodeInteractiveBlocker[] }>()
  // What the open force dialog is about (forceKey); it closes once the node is past that.
  const [confirmingForce, setConfirmingForce] = useState<string>()
  const { runId, nodeId, status, session } = target

  if (!starter) {
    return <p className="px-1 py-2 text-xs text-muted-foreground">{t('workflowRun.v2.interactive.readOnly')}</p>
  }

  const act = async (action: () => Promise<void>): Promise<boolean> => {
    setBusy(true)
    try {
      await action()
      return true
    } catch (error) {
      const blockers = error instanceof ContractError && error.code === 'workflow_node_incomplete' ? refusedBlockers(error) : []
      if (blockers.some((b) => !participation.has(b.kind))) {
        // Listed under the conversation instead: a toast would only repeat them, over the
        // "仍然完成" they offer.
        setRefused({ turns: session?.turns ?? 0, blockers })
      } else {
        // Including a refusal that names only what the card never lists (participation): the
        // click must still say why nothing happened.
        toast.error(refusal(error, t))
      }
      return false
    } finally {
      setBusy(false)
      void queryClient.invalidateQueries({ queryKey: runKeys.run(tenantId, runId) })
    }
  }
  const complete = (completion: NodeCompletion) => act(() => completeNode(tenantId, runId, nodeId, completion))

  const awaiting = status === 'awaiting_input'
  const ended = session?.sessionEnded === true
  const text = draft.trim()
  const canSend = awaiting && !ended && !busy && text !== '' && draft.length <= maxTurnLength
  const send = async () => {
    if (!canSend) return
    const message = draft
    await act(async () => {
      // v2 numbers the starter's messages as it accepts them and answers with this one's number.
      // The run on screen cannot tell: another view of the starter's may have sent in between.
      const number = await sendNodeTurn(tenantId, runId, nodeId, message)
      setLastSent({ text: message, number })
      setDraft('')
    })
  }
  // The send itself succeeded; v2 reports afterwards, on the parked node, that the message never
  // ran. A summary read before this view's last send is about an earlier message: its notice says
  // nothing about the one just sent, so it is not shown.
  const behind = lastSent !== undefined && (session?.sent ?? 0) < lastSent.number
  const undelivered = awaiting && !behind && session?.lastTurnError === 'not_delivered'
  // Only the message the notice is about goes back into the composer.
  const restorable = undelivered && !ended && lastSent !== undefined && lastSent.number === session?.sent && draft === ''
  const blockers = session?.blockers ?? []
  const unconfirmed = blockers.some((b) => b.kind === 'turn_unconfirmed')
  // Under the agent policy a turn that ended normally also waits for the Agent's report, and then
  // for the starter to confirm that very report.
  const unreported = blockers.some((b) => b.kind === 'agent_signal_pending')
  const reported = blockers.some((b) => b.kind === 'agent_confirmation_pending') && session?.signalId !== undefined
  // What a refused completion named that the snapshot does not carry (the deliverables).
  const extra = refused !== undefined && refused.turns === (session?.turns ?? 0)
    ? refused.blockers.filter((b) => !blockers.some((own) => own.kind === b.kind && own.path === b.path))
    : []
  const shown = [...blockers, ...extra].filter((b) => !participation.has(b.kind))
  // Force covers every blocker but the opening turn's confirmation, which it never stands in for;
  // with a report still owed, confirming that turn and forcing go together.
  const forcible = awaiting && (unreported || (!unconfirmed && shown.length > 0))
  const forced: NodeCompletion = unreported && unconfirmed ? { confirmInitialTurn: true, force: true } : { force: true }
  const confirmable = awaiting && !unreported && (unconfirmed || reported)
  const confirmed: NodeCompletion = {
    ...(unconfirmed ? { confirmInitialTurn: true } : {}),
    ...(reported ? { confirmSignalId: session.signalId } : {}),
  }
  const overridden = [...shown, ...blockers.filter((b) => b.kind === 'agent_signal_pending' || (b.kind === 'agent_confirmation_pending' && shown.length > 0))]
  const source = session?.answerSource ?? 'none'
  const forceKey = forcible ? JSON.stringify([session?.turns ?? 0, session?.signalId, source, overridden.map((b) => [b.kind, b.path])]) : undefined
  if (confirmingForce !== undefined && confirmingForce !== forceKey) setConfirmingForce(undefined)
  const ready = awaiting && blockers.length === 0 && session?.completable === true
  const filesOnly = shown.length > 0 && shown.every((b) => b.kind === 'file_missing' || b.kind === 'file_unchanged')

  return (
    <div className="flex w-full flex-col gap-2 border-t pt-2" data-interactive-actions="">
      <p className="text-[11px] leading-4 text-muted-foreground">{t('workflowRun.v2.interactive.partialNote')}</p>
      {undelivered && (
        <div role="status" className="flex items-center justify-between gap-2 text-xs text-warning" data-interactive-undelivered="">
          <span>{t(ended ? 'workflowRun.v2.interactive.undeliveredEnded' : 'workflowRun.v2.interactive.undelivered')}</span>
          {restorable && (
            <Button type="button" variant="outline" size="xs" disabled={busy} onClick={() => setDraft(lastSent.text)}>
              {t('workflowRun.v2.interactive.restoreDraft')}
            </Button>
          )}
        </div>
      )}
      {awaiting && !ended && (
        <div className="flex items-end gap-2">
          <Textarea
            aria-label={t('workflowRun.v2.interactive.composerLabel')}
            placeholder={t('workflowRun.v2.interactive.composerPlaceholder')}
            className="max-h-40 min-h-16 flex-1 resize-none text-sm"
            value={draft}
            maxLength={maxTurnLength}
            disabled={busy}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              event.stopPropagation()
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault()
                void send()
              }
            }}
          />
          <Button type="button" size="sm" disabled={!canSend} onClick={() => void send()}>
            {t('workflowRun.v2.interactive.send')}
          </Button>
        </div>
      )}
      {/* The undelivered notice already says the session ended, and why it matters here. */}
      {awaiting && ended && !undelivered && <p className="text-xs text-muted-foreground">{t('workflowRun.v2.interactive.sessionEnded')}</p>}
      {awaiting && (unconfirmed || unreported || reported) && shown.length === 0 && (
        <p className="text-xs text-muted-foreground" data-interactive-confirm-hint="">
          {t(`workflowRun.v2.interactive.${unreported ? 'unreportedHint' : reported ? 'reportedHint' : 'confirmHint'}`)}
        </p>
      )}
      {awaiting && shown.length > 0 && (
        <div className="flex flex-col gap-0.5 text-xs text-warning" data-interactive-blockers="">
          {shown.map((b) => (
            <p key={`${b.kind}:${b.path ?? ''}`}>{blockerText(b, t)}</p>
          ))}
          <p className="text-muted-foreground">
            {t(filesOnly ? 'workflowRun.v2.interactive.filesHint' : ended ? 'workflowRun.v2.interactive.blockedEndedHint' : 'workflowRun.v2.interactive.blockedHint')}
          </p>
        </div>
      )}
      <div className="flex items-center justify-end gap-2">
        {status === 'running' && (
          <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void act(() => stopNodeTurn(tenantId, runId, nodeId))}>
            {t('workflowRun.v2.interactive.stop')}
          </Button>
        )}
        {forcible && (
          <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setConfirmingForce(forceKey)}>
            {t('workflowRun.v2.interactive.completeAnyway')}
          </Button>
        )}
        {confirmable && (
          <Button type="button" size="sm" disabled={busy} onClick={() => void complete(confirmed)}>
            {t('workflowRun.v2.interactive.confirmComplete')}
          </Button>
        )}
        {awaiting && !confirmable && !unconfirmed && !forcible && (
          <Button type="button" size="sm" variant={ready ? 'default' : 'outline'} disabled={busy || !ready} onClick={() => void complete({})}>
            {t('workflowRun.v2.interactive.complete')}
          </Button>
        )}
      </div>
      <AlertDialog open={confirmingForce !== undefined} onOpenChange={(open) => { if (!open) setConfirmingForce(undefined) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('workflowRun.v2.interactive.forceTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('workflowRun.v2.interactive.forceBody', {
                blockers: overridden.map((b) => blockerText(b, t)).join(' '),
                source: t(`workflowRun.v2.interactive.forceSource.${source}`, { defaultValue: source }),
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>{t('workflowRun.v2.interactive.forceCancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={busy}
              onClick={() => {
                // The dialog may still be closing when its node moved on.
                if (confirmingForce === undefined) return
                setConfirmingForce(undefined)
                void complete(forced)
              }}
            >
              {t('workflowRun.v2.interactive.forceConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
