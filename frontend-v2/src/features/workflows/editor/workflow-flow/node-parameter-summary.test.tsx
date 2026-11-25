import "@/lib/workflows/test/dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { UserContext } from "@/app/auth/user";
import { createContractsClient } from "@/lib/workflows/data/contracts-client";
import { ContractsClientProvider } from "@/lib/workflows/data/contracts-client-provider";
import { LocalWorkflowBackend } from "@/lib/workflows/data/local-workflow-backend";
import type { WorkflowLoopConfig, WorkflowNodeData } from "@/lib/workflows/mock";
import { AppI18nProvider } from "@/lib/workflows/test/i18n";
import { appI18n } from "@/lib/workflows/test/i18n-instance";
import { localWorkflowContracts, memoryWorkflowStorage } from "@/lib/workflows/test/workflow-server";
import { demoUser, fakeBackend } from "@/test/backend";
import { WorkflowNodeParameterSummary } from "./node-parameter-summary";

function renderSummary(data: WorkflowNodeData) {
  fakeBackend(() => undefined);
  const backend = new LocalWorkflowBackend({ storage: memoryWorkflowStorage() });
  const client = createContractsClient(localWorkflowContracts(backend), "summary-test");
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <ContractsClientProvider client={client}>
        <UserContext.Provider value={demoUser}>
          <AppI18nProvider>
            <WorkflowNodeParameterSummary data={data} />
          </AppI18nProvider>
        </UserContext.Provider>
      </ContractsClientProvider>
    </QueryClientProvider>,
  );
}

const carried = {
  name: "value",
  valueType: "string",
  initial: { kind: "constant", value: "KEEP" },
  feedback: ["loop", "value"],
};

describe("WorkflowNodeParameterSummary", () => {
  it("labels an empty Agent executor as no Agent selected in both languages", async () => {
    const data: WorkflowNodeData = {
      kind: "agent",
      title: "Agent",
      description: "",
      agentConfig: {
        schemaVersion: 3,
        executor: { agentCli: "", modelId: "" },
        roleId: "",
        skills: [],
        mcps: [],
        prompt: "hi",
      },
    };
    await appI18n.changeLanguage("zh-CN");
    const { unmount } = renderSummary(data);
    expect(screen.getByText("未选择智能体")).toBeInTheDocument();
    expect(screen.queryByText("— · —")).not.toBeInTheDocument();
    unmount();

    await appI18n.changeLanguage("en-US");
    renderSummary(data);
    expect(screen.getByText("No Agent selected")).toBeInTheDocument();
    expect(screen.queryByText("— · —")).not.toBeInTheDocument();
  });

  it("renders populated and partial Agent executors as agentCli · modelId", async () => {
    await appI18n.changeLanguage("zh-CN");
    const { unmount } = renderSummary({
      kind: "agent",
      title: "Agent",
      description: "",
      agentConfig: {
        schemaVersion: 3,
        executor: { agentCli: "official/ora-space.opencode", modelId: "fixture" },
        roleId: "",
        skills: [],
        mcps: [],
        prompt: "hi",
      },
    });
    expect(screen.getByText("official/ora-space.opencode · fixture")).toBeInTheDocument();
    unmount();

    renderSummary({
      kind: "agent",
      title: "Agent",
      description: "",
      agentConfig: {
        schemaVersion: 3,
        executor: { agentCli: "official/ora-space.opencode", modelId: "" },
        roleId: "",
        skills: [],
        mcps: [],
        prompt: "hi",
      },
    });
    expect(screen.getByText("official/ora-space.opencode · —")).toBeInTheDocument();
  });

  it.each<[string, object, string | undefined, string | undefined]>([
    ["is complete", {}, "3", "KEEP"],
    ["starts from another variable", { variables: [{ ...carried, initial: { kind: "variable", selector: ["a", "b"] } }] }, "3", "a.b"],
    ["has no variables", { variables: undefined }, "3", undefined],
    ["has null variables", { variables: null }, "3", undefined],
    ["has variables that are no list", { variables: {} }, "3", undefined],
    ["has no named variable", { variables: [null, {}, { name: 1 }] }, "3", undefined],
    ["has an unnamed variable first", { variables: [null, {}, carried] }, "3", "KEEP"],
    ["has a variable without an initial value", { variables: [{ ...carried, initial: undefined }] }, "3", undefined],
    ["has a variable with a null initial value", { variables: [{ ...carried, initial: null }] }, "3", undefined],
    ["has a variable that reads no selector", { variables: [{ ...carried, initial: { kind: "variable" } }] }, "3", undefined],
    ["has no limit", { maxIterations: undefined }, undefined, "KEEP"],
    ["has a null limit", { maxIterations: null }, undefined, "KEEP"],
  ])("summarizes a Loop that %s", async (_, changed, limit, initial) => {
    await appI18n.changeLanguage("zh-CN");
    renderSummary({
      kind: "loop",
      title: "循环",
      description: "",
      loopConfig: {
        maxIterations: 3,
        variables: [carried],
        until: { logic: "and", conditions: [] },
        outputs: [],
        ...changed,
      } as unknown as WorkflowLoopConfig,
    });
    for (const [label, value] of [["最大轮次", limit], ["初始值", initial]] as const) {
      if (value === undefined) {
        expect(screen.queryByText(label)).not.toBeInTheDocument();
      } else {
        expect(screen.getByText(label)).toBeInTheDocument();
        expect(screen.getByText(value)).toBeInTheDocument();
      }
    }
  });
});
