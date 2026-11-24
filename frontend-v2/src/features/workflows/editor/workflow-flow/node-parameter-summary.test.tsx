import "@/lib/workflows/test/dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { UserContext } from "@/app/auth/user";
import { createContractsClient } from "@/lib/workflows/data/contracts-client";
import { ContractsClientProvider } from "@/lib/workflows/data/contracts-client-provider";
import { LocalWorkflowBackend } from "@/lib/workflows/data/local-workflow-backend";
import type { WorkflowNodeData } from "@/lib/workflows/mock";
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
});
