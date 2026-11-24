import { useState } from "react";
import { useTranslation } from "react-i18next";
import { IconAlertTriangle, IconLoader2 } from "@tabler/icons-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Label,
  Textarea,
} from "@/lib/workflows/host/ui";
import type { WorkflowInputVariable } from "@/lib/workflows/mock";
import { WorkflowStartVariableFields } from "@/lib/workflows/host/start-variable-fields";
import { workflowStartDrafts, parseWorkflowStartDrafts } from "@/lib/workflows/host/start-variable-drafts";
import { WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT } from "@/lib/workflows/personal-skills";

/** A reachable node start will refuse, named as the author titled it. */
export interface WorkflowRunUnsupportedNode {
  nodeId: string;
  kind: string;
  title: string;
}

interface WorkflowRunStartDialogProps {
  open: boolean;
  runName: string;
  /** The free-text run instruction, edited here as the workflow's "initial prompt". */
  initialPrompt: string;
  variables: WorkflowInputVariable[];
  /**
   * Reachable nodes v2-server's analysis says start will refuse. The dialog warns but still lets
   * the author start, so the refusal stays the server's.
   */
  unsupportedNodes?: readonly WorkflowRunUnsupportedNode[];
  /** Prevents submission until the server has checked the frozen execution path. */
  analyzing?: boolean;
  /** Keeps a failed preflight visible while server start remains the authority. */
  analysisFailed?: boolean;
  /** True when any Agent node in the frozen snapshot enables a personal Skill. */
  hasPersonalSkills?: boolean;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onStart: (
    initialPrompt: string,
    variables: Record<string, unknown>,
  ) => Promise<void>;
}

/** Collects the run instruction and deployment-time Start values before execution. */
export function WorkflowRunStartDialog({
  open,
  runName,
  initialPrompt,
  variables,
  unsupportedNodes = [],
  analyzing = false,
  analysisFailed = false,
  hasPersonalSkills = false,
  busy,
  onOpenChange,
  onStart,
}: WorkflowRunStartDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      {open && (
        <WorkflowRunStartDialogContent
          runName={runName}
          initialPrompt={initialPrompt}
          variables={variables}
          unsupportedNodes={unsupportedNodes}
          analyzing={analyzing}
          analysisFailed={analysisFailed}
          hasPersonalSkills={hasPersonalSkills}
          busy={busy}
          onCancel={() => onOpenChange(false)}
          onStart={onStart}
        />
      )}
    </Dialog>
  );
}

/** Keeps form drafts local so closing the dialog leaves persisted run values untouched. */
function WorkflowRunStartDialogContent({
  runName,
  initialPrompt,
  variables,
  unsupportedNodes,
  analyzing,
  analysisFailed,
  hasPersonalSkills,
  busy,
  onCancel,
  onStart,
}: Omit<
  WorkflowRunStartDialogProps,
  "open" | "onOpenChange" | "unsupportedNodes"
> & {
  unsupportedNodes: readonly WorkflowRunUnsupportedNode[];
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  const [initialPromptDraft, setInitialPromptDraft] = useState(initialPrompt);
  const [drafts, setDrafts] = useState<Record<string, string>>(() =>
    workflowStartDrafts(variables),
  );
  const [attemptedStart, setAttemptedStart] = useState(false);
  const parsed = parseWorkflowStartDrafts(variables,drafts);
  const hasInvalidValue = parsed.some(
    ({ result, exceedsMaxLength, missingRequired }) =>
      !result.valid || exceedsMaxLength || missingRequired,
  );

  async function submit(): Promise<void> {
    setAttemptedStart(true);
    if (hasInvalidValue) return;
    const values = Object.fromEntries(
      parsed.map(({ variable, result }) => [
        variable.name,
        result.valid && result.value !== undefined ? result.value : null,
      ]),
    );
    await onStart(initialPromptDraft, values);
  }

  return (
    <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-lg">
      <DialogHeader className="border-b border-border px-5 py-4">
        <DialogTitle>{runName}</DialogTitle>
        <DialogDescription>
          {t("workflowRun.startInputsDescription")}
        </DialogDescription>
      </DialogHeader>
      <div className="max-h-[min(62vh,560px)] space-y-4 overflow-y-auto px-5 py-5">
        {analyzing && (
          <p role="status" className="text-xs text-muted-foreground">
            {t("workflowRun.unsupportedNodes.checking")}
          </p>
        )}
        {analysisFailed && (
          <p role="alert" className="text-xs text-muted-foreground">
            {t("workflowRun.unsupportedNodes.failed")}
          </p>
        )}
        {unsupportedNodes.length > 0 && (
          <UnsupportedNodesWarning nodes={unsupportedNodes} />
        )}
        {hasPersonalSkills && (
          <p className="text-xs leading-relaxed text-muted-foreground">
            {WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT}
          </p>
        )}
        <WorkflowStartVariableFields variables={variables} drafts={drafts} setDrafts={setDrafts} attemptedStart={attemptedStart} />
        <div className="space-y-1.5">
          <div className="flex items-baseline justify-between gap-2">
            <Label htmlFor="workflow-run-start-prompt">
              {t("settings.workflow.field.initialPrompt")}
            </Label>
            <span className="text-[10px] text-muted-foreground">
              {t("workflowRun.inputOptional")}
            </span>
          </div>
          <Textarea
            id="workflow-run-start-prompt"
            className="min-h-24 bg-muted/55"
            value={initialPromptDraft}
            onChange={(event) => setInitialPromptDraft(event.target.value)}
          />
        </div>
      </div>
      <DialogFooter className="mb-0 border-t border-border px-5 py-4">
        <Button
          type="button"
          variant="outline"
          className="min-w-24"
          disabled={busy}
          onClick={onCancel}
        >
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          className="min-w-28"
          disabled={busy || analyzing}
          onClick={() => void submit()}
        >
          {busy ? (
            <span className="inline-flex items-center gap-1.5">
              <IconLoader2 className="size-3.5 animate-spin" />
              {t("workflowRun.starting")}
            </span>
          ) : (
            t("workflowRun.startConfirm")
          )}
        </Button>
      </DialogFooter>
    </DialogContent>
  );
}

/** Names each reachable node start will refuse, before the author tries. */
function UnsupportedNodesWarning({
  nodes,
}: {
  nodes: readonly WorkflowRunUnsupportedNode[];
}) {
  const { t } = useTranslation();
  return (
    <div
      role="alert"
      className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-xs text-amber-900 dark:text-amber-200"
    >
      <p className="flex items-center gap-1.5 font-medium">
        <IconAlertTriangle aria-hidden className="size-3.5 shrink-0" />
        {t("workflowRun.unsupportedNodes.title")}
      </p>
      <p className="mt-1 leading-5">
        {t("workflowRun.unsupportedNodes.description")}
      </p>
      <ul className="mt-1.5 list-disc space-y-0.5 pl-5">
        {nodes.map((node) => (
          <li key={node.nodeId}>
            {t("workflowRun.unsupportedNodes.item", {
              title: node.title,
              kind: t(`workflowRun.unsupportedNodes.kind.${node.kind}`, {
                defaultValue: node.kind,
              }),
            })}
          </li>
        ))}
      </ul>
    </div>
  );
}
