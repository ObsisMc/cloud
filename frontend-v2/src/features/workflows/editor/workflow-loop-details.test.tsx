import { fireEvent, render, screen } from "@testing-library/react";
import type { Node } from "@xyflow/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import {
  createMockWorkflowCapabilities,
  type WorkflowLoopConfig,
  type WorkflowNodeData,
} from "@/lib/workflows/mock";
import { AppI18nProvider } from "@/lib/workflows/test/i18n";
import { appI18n } from "@/lib/workflows/test/i18n-instance";
import { WorkflowInspector } from "./workflow-inspector";

const value = {
  name: "value",
  valueType: "string",
  initial: { kind: "constant", value: "KEEP" },
  feedback: ["loop", "value"],
};
const other = { ...value, name: "other", initial: { kind: "constant", value: "OTHER" } };
const complete = {
  maxIterations: 3,
  variables: [value, other],
  until: {
    logic: "or",
    conditions: [
      { variableSelector: ["loop", "value"], operator: "equals", value: "DONE" },
      { variableSelector: ["loop", "other"], operator: "equals", value: "STOP" },
    ],
  },
  outputs: [],
};

/** Selects a Loop whose draft holds `loopConfig`, as an imported draft may. */
async function selectLoop(loopConfig: object) {
  await appI18n.changeLanguage("zh-CN");
  const onUpdate = vi.fn<(node: Node<WorkflowNodeData, "workflow">) => void>();
  const node: Node<WorkflowNodeData, "workflow"> = {
    id: "loop",
    type: "workflow",
    position: { x: 0, y: 0 },
    data: { kind: "loop", title: "循环", description: "", loopConfig: loopConfig as WorkflowLoopConfig },
  };
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AppI18nProvider>
        <WorkflowInspector
          variableCatalog={[]}
          node={node}
          capabilities={createMockWorkflowCapabilities("zh-CN")}
          onUpdate={onUpdate}
          onDelete={() => undefined}
          onCloseNode={() => undefined}
        />
      </AppI18nProvider>
    </QueryClientProvider>,
  );
  return {
    initial: screen.getByLabelText<HTMLInputElement>("初始值"),
    conditions: screen.queryAllByLabelText(/^值/).length,
    updated: () => onUpdate.mock.lastCall?.[0].data.loopConfig as unknown,
  };
}

const without = (key: keyof typeof complete) =>
  Object.fromEntries(Object.entries(complete).filter(([name]) => name !== key));

describe("the Loop panel", () => {
  it("shows a complete Loop and edits its first variable in place", async () => {
    const panel = await selectLoop(complete);
    expect(panel.initial).toHaveValue("KEEP");
    expect(panel.conditions).toBe(2);
    expect(screen.getAllByText("OR")).not.toHaveLength(0);
    fireEvent.change(panel.initial, { target: { value: "NEXT" } });
    expect(panel.updated()).toEqual({
      ...complete,
      variables: [{ ...value, initial: { kind: "constant", value: "NEXT" } }, other],
    });
  });

  it.each([
    ["missing", without("variables"), "", true],
    ["null", { ...complete, variables: null }, "", true],
    ["an object", { ...complete, variables: {} }, "", true],
    ["without a named entry", { ...complete, variables: [null, {}, { name: 1 }, { name: " " }] }, "", true],
    ["with an unnamed entry first", { ...complete, variables: [{}, value] }, "KEEP", false],
    ["with a null entry", { ...complete, variables: [value, null] }, "KEEP", false],
    ["of no type", { ...complete, variables: [{ ...value, valueType: null }] }, "KEEP", false],
    ["of a type that is no text", { ...complete, variables: [{ ...value, valueType: 3 }] }, "KEEP", false],
    ["without an initial value", { ...complete, variables: [{ ...value, initial: undefined }] }, "", false],
    ["with a null initial value", { ...complete, variables: [{ ...value, initial: null }] }, "", false],
    ["that start from another one", { ...complete, variables: [{ ...value, initial: { kind: "variable", selector: ["a", "b"] } }] }, "", false],
  ])("reads variables %s", async (_, loopConfig, shown, disabled) => {
    const panel = await selectLoop(loopConfig);
    expect(panel.initial).toHaveValue(shown);
    expect(panel.initial.disabled).toBe(disabled);
  });

  it("keeps the entries it does not edit", async () => {
    const panel = await selectLoop({ ...complete, variables: [null, {}, { ...value, initial: null }, other] });
    fireEvent.change(panel.initial, { target: { value: "NEXT" } });
    expect(panel.updated()).toEqual({
      ...complete,
      variables: [null, {}, { ...value, initial: { kind: "constant", value: "NEXT" } }, other],
    });
  });

  it.each([
    ["missing", without("until")],
    ["null", { ...complete, until: null }],
    ["empty", { ...complete, until: {} }],
    ["without a list of conditions", { ...complete, until: { logic: "and", conditions: {} } }],
  ])("reads an end condition that is %s", async (_, loopConfig) => {
    const panel = await selectLoop(loopConfig);
    expect(panel.conditions).toBe(0);
    fireEvent.click(screen.getByRole("button", { name: /添加/ }));
    expect(panel.updated()).toEqual({
      ...loopConfig,
      until: { logic: "and", conditions: [{ variableSelector: [], operator: "" }] },
    });
  });

  it("reads a Loop without a limit", async () => {
    await selectLoop(without("maxIterations"));
    expect(screen.getByLabelText("最大轮次")).toHaveValue(3);
  });
});
