import "@/lib/workflows/test/dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { describe, expect, it, vi } from "vitest";
import { UserContext } from "@/app/auth/user";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { WorkflowAgentModelsCatalog } from "@/lib/workflows/data/catalog-hooks";
import { createContractsClient } from "@/lib/workflows/data/contracts-client";
import { ContractsClientProvider } from "@/lib/workflows/data/contracts-client-provider";
import { LocalWorkflowBackend } from "@/lib/workflows/data/local-workflow-backend";
import { workflowI18n } from "@/lib/workflows/host/i18n";
import { createMockWorkflowCapabilities } from "@/lib/workflows/mock";
import { localWorkflowContracts, memoryWorkflowStorage } from "@/lib/workflows/test/workflow-server";
import { demoUser, fakeBackend } from "@/test/backend";
import { WorkflowEditor } from "./workflow-editor";
import { useWorkflowEditorStore } from "./workflow-editor-store";

let agentModelsCatalogOverride: WorkflowAgentModelsCatalog | null = null;

vi.mock("@/lib/workflows/data/catalog-hooks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/workflows/data/catalog-hooks")>();
  return {
    ...actual,
    useWorkflowAgentModels: () => {
      const real = actual.useWorkflowAgentModels();
      return agentModelsCatalogOverride ?? real;
    },
  };
});

const tenantId = "model-hydration";
const agent = "official/ora-space.opencode";
const catalog = [
  { id: "other-agent", name: "Other Agent · Foreign", agentId: "another-agent", agentName: "Other Agent", modelId: "foreign", modelName: "Foreign" },
  { id: "first", name: "OpenCode · Current", agentId: agent, agentName: "OpenCode", modelId: "configured", modelName: "Current" },
  { id: "second", name: "OpenCode · Second", agentId: agent, agentName: "OpenCode", modelId: "second", modelName: "Second" },
];

/** Mounts the real editor with an optional pre-seeded or deferred executor catalog. */
async function openEditor(
  modelId: string,
  options: {
    models?: typeof catalog | null;
    agentCli?: string;
    override?: boolean;
    extraNodes?: unknown[];
    onList?: (client: ReturnType<typeof createContractsClient>) => void;
  } = {},
) {
  fakeBackend(() => undefined);
  const backend = new LocalWorkflowBackend({ storage: memoryWorkflowStorage() });
  const initialExecutor = { agentCli: options.agentCli ?? agent, modelId };
  const created = backend.create({ name: "Model hydration", graph: JSON.stringify({
    nodes: [
      { id: "start", type: "workflow", position: { x: 0, y: 0 }, data: { kind: "start", title: "开始" } },
      { id: "agent", type: "workflow", position: { x: 320, y: 0 }, data: { kind: "agent", title: "模型测试", agentConfig: {
        ...createMockWorkflowCapabilities("zh-CN").defaultAgentConfig, executor: initialExecutor,
      } } },
      ...(options.extraNodes ?? []),
    ],
    edges: [{ id: "edge", source: "start", target: "agent" }],
  }) });
  const workflowId = created.workflow.id;
  const update = vi.spyOn(backend, "updateDraft");
  const client = createContractsClient(localWorkflowContracts(backend), tenantId);
  if (options.models === null && options.onList) {
    options.onList(client);
  }
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (options.models !== null) {
    queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: options.models ?? catalog });
  }
  useWorkflowEditorStore.setState({ selectedWorkflowId: workflowId, managerError: null });
  render(<I18nextProvider i18n={workflowI18n}><QueryClientProvider client={queries}>
    <ContractsClientProvider client={client}><UserContext.Provider value={demoUser}><TooltipProvider>
      <WorkflowEditor {...(options.override ? { capabilities: createMockWorkflowCapabilities("zh-CN") } : {})} />
    </TooltipProvider></UserContext.Provider></ContractsClientProvider>
  </QueryClientProvider></I18nextProvider>);
  await screen.findByDisplayValue("Model hydration");
  await screen.findByLabelText("工作流画布");
  const savedExecutor = () => JSON.parse(backend.getDraft({ workflowId }).snapshot.graph).nodes
    .find((node: { id: string }) => node.id === "agent").data.agentConfig.executor;
  return { backend, workflowId, queries, client, update, initialExecutor, savedExecutor };
}

describe("editor model hydration", () => {
  it.each([
    { name: "stale model", model: "deepseek/deepseek-v4-flash", expected: "configured" },
    { name: "blank model", model: "", expected: "configured" },
    { name: "available non-first model", model: "second", expected: "second" },
    { name: "model of another Agent", model: "foreign", expected: "configured" },
    { name: "missing Agent", model: "stale", expected: "stale", agentCli: "missing-agent" },
    { name: "empty catalog", model: "stale", expected: "stale", models: [] },
    { name: "capability override", model: "stale", expected: "stale", override: true },
  ])("hydrates $name and only persists on a later save", async ({ model, expected, ...options }) => {
    const state = await openEditor(model, options);
    expect(screen.queryByText("未保存")).not.toBeInTheDocument();
    expect(state.update).not.toHaveBeenCalled();
    expect(state.savedExecutor()).toEqual(state.initialExecutor);
    // Like desktop, hydration is a clean baseline. Publishing first forces a save of that baseline.
    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    expect(state.savedExecutor()).toEqual({ agentCli: options.agentCli ?? agent, modelId: expected });
  });

  it("re-runs stale model hydration when the catalog arrives late without dirtying the clean draft", async () => {
    const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
    expect(screen.queryByText("未保存")).not.toBeInTheDocument();
    expect(state.update).not.toHaveBeenCalled();

    state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
    await waitFor(() => expect(screen.queryByText("未保存")).not.toBeInTheDocument());
    expect(state.update).not.toHaveBeenCalled();
    expect(state.savedExecutor()).toEqual(state.initialExecutor);

    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });
  });

  it("defaults a new Agent node to the catalog's first model", async () => {
    const state = await openEditor("configured");
    fireEvent.click(screen.getByRole("button", { name: "Agent" }));
    expect(await screen.findByText("未保存")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalled());
    const nodes = JSON.parse(state.backend.getDraft({ workflowId: state.workflowId }).snapshot.graph).nodes;
    const added = nodes.find((node: { id: string; data: { kind: string } }) => node.data.kind === "agent" && node.id !== "agent");
    expect(added.data.agentConfig.executor).toEqual({ agentCli: catalog[0]!.agentId, modelId: catalog[0]!.modelId });
  });

  it("leaves a new Agent executor blank when the catalog is empty and fills it once the catalog loads", async () => {
    const state = await openEditor("stale", { models: [] });
    fireEvent.click(screen.getByRole("button", { name: "Agent" }));
    expect(await screen.findByText("未保存")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalledTimes(1));
    let nodes = JSON.parse(state.backend.getDraft({ workflowId: state.workflowId }).snapshot.graph).nodes;
    let added = nodes.find((node: { id: string; data: { kind: string } }) => node.data.kind === "agent" && node.id !== "agent");
    expect(added.data.agentConfig.executor).toEqual({ agentCli: "", modelId: "" });

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
    await waitFor(() => expect(screen.getAllByText(/another-agent · foreign|Foreign/).length).toBeGreaterThan(0));
    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalledTimes(2));
    nodes = JSON.parse(state.backend.getDraft({ workflowId: state.workflowId }).snapshot.graph).nodes;
    added = nodes.find((node: { id: string; data: { kind: string } }) => node.data.kind === "agent" && node.id !== "agent");
    expect(added.data.agentConfig.executor).toEqual({ agentCli: catalog[0]!.agentId, modelId: catalog[0]!.modelId });
  });

  it("re-runs stale model hydration after an initial executor catalog error recovers", async () => {
    let listSpy: ReturnType<typeof vi.spyOn> | undefined;
    const state = await openEditor("deepseek/deepseek-v4-flash", {
      models: null,
      onList: (client) => {
        listSpy = vi
          .spyOn(client.executors, "list")
          .mockRejectedValueOnce(new Error("executors unavailable"))
          .mockResolvedValueOnce({ items: catalog });
      },
    });
    await waitFor(() => expect(listSpy).toHaveBeenCalledTimes(1));
    expect(screen.queryByText("未保存")).not.toBeInTheDocument();
    expect(state.update).not.toHaveBeenCalled();

    await state.queries.refetchQueries({ queryKey: ["workflow-catalog", "executors", tenantId] });
    await waitFor(() => expect(listSpy).toHaveBeenCalledTimes(2));
    expect(screen.queryByText("未保存")).not.toBeInTheDocument();
    expect(state.update).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });
  });

  it("keeps prior edits, undo history, and late model repairs intact when undoing after a late catalog arrives", async () => {
    const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
    fireEvent.click(screen.getByRole("button", { name: "Agent" }));
    expect(await screen.findByText("未保存")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "撤销" })).toBeEnabled();

    state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
    await waitFor(() => expect(screen.getAllByText(/another-agent · foreign|Foreign/).length).toBeGreaterThan(0));
    expect(screen.getByRole("button", { name: "撤销" })).toBeEnabled();
    expect(screen.getByText("未保存")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "撤销" }));
    await waitFor(() => expect(screen.queryByText(/another-agent · foreign|Foreign/)).not.toBeInTheDocument());
    expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    const savedNodes = JSON.parse(state.backend.getDraft({ workflowId: state.workflowId }).snapshot.graph).nodes as {
      id: string;
      data: { kind: string; agentConfig?: { executor: { agentCli: string; modelId: string } } };
    }[];
    expect(savedNodes.filter((node) => node.data.kind === "agent")).toHaveLength(1);
    expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });
  });

  it("does not overwrite an Agent executor the user already changed before a late catalog arrives", async () => {
    const user = userEvent.setup();
    agentModelsCatalogOverride = {
      agents: [{ agentRef: "another-agent", label: "Other Agent", logo: null }],
      agentModels: [],
      modelsByCli: new Map(),
      cliStatus: { "another-agent": { isLoading: true, isError: false } },
      isLoading: true,
      isError: false,
      refetch: vi.fn(),
    };
    try {
      const state = await openEditor("deepseek/deepseek-v4-flash", {
        models: null,
        extraNodes: [
          {
            id: "untouched",
            type: "workflow",
            position: { x: 640, y: 0 },
            data: {
              kind: "agent",
              title: "未修改节点",
              agentConfig: {
                ...createMockWorkflowCapabilities("zh-CN").defaultAgentConfig,
                executor: { agentCli: agent, modelId: "deepseek/deepseek-v4-flash" },
              },
            },
          },
        ],
      });

      const nodeCard = document.querySelector<HTMLElement>('.react-flow__node[data-id="agent"]');
      expect(nodeCard).not.toBeNull();
      await user.click(nodeCard!);

      const inspector = await waitFor(() => {
        const aside = document.querySelector<HTMLElement>("[data-workflow-inspector]");
        expect(aside).not.toBeNull();
        return aside!;
      });
      await user.click(within(inspector).getByRole("button", { name: "Agent 模型" }));
      await user.click(await screen.findByRole("option", { name: "Other Agent" }));
      expect(await screen.findByText("未保存")).toBeInTheDocument();

      agentModelsCatalogOverride = null;
      state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });

      await waitFor(() =>
        expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0),
      );
      fireEvent.click(screen.getByRole("button", { name: "发布" }));
      await screen.findByRole("alertdialog", { name: "发布工作流" });
      await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
      const savedNodes = JSON.parse(state.backend.getDraft({ workflowId: state.workflowId }).snapshot.graph).nodes as {
        id: string;
        data: { agentConfig: { executor: { agentCli: string; modelId: string } } };
      }[];
      expect(savedNodes.find((node) => node.id === "agent")?.data.agentConfig.executor).toEqual({
        agentCli: "another-agent",
        modelId: "deepseek/deepseek-v4-flash",
      });
      expect(savedNodes.find((node) => node.id === "untouched")?.data.agentConfig.executor).toEqual({
        agentCli: agent,
        modelId: "configured",
      });
    } finally {
      agentModelsCatalogOverride = null;
    }
  });
});
