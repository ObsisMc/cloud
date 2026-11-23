import { useQuery } from "@tanstack/react-query";
import { getWorkflow } from "./api/generated";
import { parseWorkflowGraph, type WorkflowDefinitionNode } from "./runtime";

/**
 * Team visibility warning shown wherever a workflow referencing personal Skills is configured or
 * launched: v2 resolves personal Skill names against the run starter's catalog, and node sessions
 * are readable by the whole workspace.
 */
export const WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT =
  "运行时读取发起人同名的个人 Skill，其内容在节点会话中对团队可见。";

/** Reports whether a Skill identifier refers to a personal Skill (plain name, not `namespace/id`). */
export function isPersonalSkillId(skillId: string): boolean {
  const trimmed = skillId.trim();
  return trimmed !== "" && !trimmed.includes("/");
}

/** Reports whether any Agent node in `nodes` enables at least one personal Skill. */
export function hasEnabledPersonalSkills(nodes: readonly WorkflowDefinitionNode[]): boolean {
  for (const node of nodes) {
    if (node.data.kind !== "agent") continue;
    const skills = node.data.agentConfig?.skills;
    if (!skills) continue;
    for (const binding of skills) {
      if (binding.enabled && isPersonalSkillId(binding.skillId)) {
        return true;
      }
    }
  }
  return false;
}

/**
 * Safely parses a serialized workflow graph and reports whether any Agent node enables a personal
 * Skill. Malformed or empty graphs return `false` so launch surfaces never break on hint rendering.
 */
export function graphHasEnabledPersonalSkills(graph: string | undefined | null): boolean {
  if (!graph) return false;
  try {
    return hasEnabledPersonalSkills(parseWorkflowGraph(graph).nodes);
  } catch {
    return false;
  }
}

/**
 * Checks whether a published workflow's active snapshot enables any personal Skill. Failures fall
 * back to `false` so non-blocking visibility copy never blocks launching or configuring a workflow.
 */
export function useWorkflowHasPersonalSkills(
  tenantId: string | undefined,
  workflowId: string | undefined,
): boolean {
  const query = useQuery({
    queryKey: ["workflow-personal-skills", tenantId, workflowId],
    enabled: Boolean(tenantId && workflowId),
    staleTime: 60_000,
    retry: false,
    queryFn: async ({ signal }) => {
      try {
        const detail = await getWorkflow(tenantId!, workflowId!, undefined, signal);
        return graphHasEnabledPersonalSkills(detail?.published?.graph);
      } catch {
        return false;
      }
    },
  });
  return query.data === true;
}
