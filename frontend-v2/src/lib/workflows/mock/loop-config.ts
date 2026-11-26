import type {
  WorkflowConditionLogic,
  WorkflowConditionComparison,
  WorkflowLoopConfig,
  WorkflowLoopVariable,
  WorkflowVariableValueType,
} from "./node-data";

// An imported draft keeps `loopConfig` as it was written, and the editor reads it before anyone can
// repair it, so reading an incomplete one must not throw.

/** The entries that declare a variable: those with a name. */
export function namedEntries<T extends { name: string }>(
  list: readonly T[] | undefined,
): T[] {
  return Array.isArray(list)
    ? list.filter(
        (entry: T | null | undefined) =>
          typeof entry?.name === "string" && entry.name.trim() !== "",
      )
    : [];
}

/** A Loop variable's type. Consumers read it as text; a draft may hold anything there. */
export function loopVariableType(
  variable: WorkflowLoopVariable,
): WorkflowVariableValueType {
  const valueType: unknown = variable.valueType;
  return typeof valueType === "string" ? variable.valueType : "any";
}

/** What a Loop variable starts from, as text: its constant or the variable it reads. */
export function loopInitialText(
  variable: WorkflowLoopVariable | undefined,
): { kind: "constant" | "variable"; text: string } | undefined {
  const initial: Partial<{ kind: string; value: unknown; selector: unknown }> | null | undefined =
    variable?.initial;
  if (initial?.kind === "constant") {
    return { kind: "constant", text: String(initial.value ?? "") };
  }
  return Array.isArray(initial?.selector)
    ? { kind: "variable", text: initial.selector.join(".") }
    : undefined;
}

/** A Loop's end condition; a missing one has no conditions. */
export function loopUntil(config: WorkflowLoopConfig): {
  logic: WorkflowConditionLogic;
  conditions: WorkflowConditionComparison[];
} {
  const until: Partial<WorkflowLoopConfig["until"]> | null | undefined = config.until;
  return {
    logic: until?.logic === "or" ? "or" : "and",
    conditions: Array.isArray(until?.conditions) ? until.conditions : [],
  };
}
