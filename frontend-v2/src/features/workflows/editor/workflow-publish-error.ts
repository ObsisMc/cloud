import type { TFunction } from "i18next";
import { ContractError } from "@/lib/workflows/contracts";
import { localizeContractError } from "@/lib/workflows/host/contract-error";

/** What the publish dialog says about a failed publish. */
export interface WorkflowPublishFailure {
  /** States that publishing (not saving) failed and, when known, which node was refused. */
  message: string;
  /** The server's diagnostic reason, shown verbatim beneath the message. */
  detail: string | null;
}

/**
 * Publish-validation field paths the editor can name in the author's terms, most specific
 * first. Paths are matched after dropping the `data.` prefix and array indices, so
 * `data.outputs[0].variableSelector` reads as the Output's results.
 */
const FIELD_LABELS: ReadonlyArray<readonly [prefix: string, key: string]> = [
  ["iterationConfig.iteratorSelector", "settings.workflow.publishField.iterator"],
  ["iterationConfig.collectSelector", "settings.workflow.publishField.collect"],
  ["iterationConfig", "settings.workflow.publishField.iteration"],
  ["aggregatorConfig", "settings.workflow.publishField.aggregator"],
  ["loopConfig.variables.initial", "settings.workflow.publishField.loopInitial"],
  ["loopConfig.variables.feedback", "settings.workflow.publishField.loopFeedback"],
  ["loopConfig.until", "settings.workflow.publishField.loopUntil"],
  ["loopConfig.outputs", "settings.workflow.publishField.loopOutputs"],
  ["loopConfig", "settings.workflow.publishField.loop"],
  ["agentConfig.executor", "settings.workflow.publishField.executor"],
  ["agentConfig.prompt", "settings.workflow.publishField.prompt"],
  ["agentConfig", "settings.workflow.publishField.agent"],
  ["outputs", "settings.workflow.publishField.outputs"],
  ["inputVariables", "settings.workflow.publishField.inputs"],
  ["cases", "settings.workflow.publishField.cases"],
  ["selector", "settings.workflow.publishField.selector"],
  ["edges", "settings.workflow.publishField.edges"],
  ["edge", "settings.workflow.publishField.edges"],
  ["parentId", "settings.workflow.publishField.container"],
  ["containerId", "settings.workflow.publishField.container"],
];

/** Names a refused field, or `null` when the editor has no words for it. */
function fieldLabel(field: string, t: TFunction): string | null {
  const path = field.replace(/^data\./, "").replace(/\[\d+\]/g, "");
  const match = FIELD_LABELS.find(
    ([prefix]) => path === prefix || path.startsWith(`${prefix}.`),
  );
  return match === undefined ? null : t(match[1]);
}

/**
 * Explains a refused publish. `workflow_run_graph_parse` carries the refused node, field and
 * reason in its params when the server knows them; the node is named by its title so the
 * author can find it on the canvas. Other refusals keep their own localized message.
 */
export function describePublishFailure(
  cause: unknown,
  nodeTitle: (nodeId: string) => string | undefined,
  t: TFunction,
): WorkflowPublishFailure {
  if (
    !(cause instanceof ContractError) ||
    cause.code !== "workflow_run_graph_parse"
  ) {
    return {
      message: t("settings.workflow.publishFailed", {
        message: localizeContractError(cause, t),
      }),
      detail: null,
    };
  }
  const { nodeId, field, reason } = cause.params;
  const detail = typeof reason === "string" && reason !== "" ? reason : null;
  if (typeof nodeId !== "string" || nodeId === "") {
    return { message: t("settings.workflow.publishRejectedGraph"), detail };
  }
  const node = nodeTitle(nodeId) ?? nodeId;
  if (typeof field !== "string" || field === "") {
    return { message: t("settings.workflow.publishRejectedNode", { node }), detail };
  }
  const label = fieldLabel(field, t);
  return {
    message:
      label === null
        ? t("settings.workflow.publishRejectedNodePath", { node, field })
        : t("settings.workflow.publishRejectedNodeField", { node, field: label }),
    detail,
  };
}
