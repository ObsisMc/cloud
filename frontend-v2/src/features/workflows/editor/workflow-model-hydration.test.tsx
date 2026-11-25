import "@/lib/workflows/test/dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

/** jsdom never runs layout, and d3 drag needs a window-scoped event view. */
function windowedMouseEvent(
  type: "mousedown" | "mousemove" | "mouseup",
  init: { button?: number; clientX: number; clientY: number },
): MouseEvent {
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    ...init,
  });
  Object.defineProperty(event, "view", { value: window });
  return event;
}

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
  const savedNode = () =>
    JSON.parse(backend.getDraft({ workflowId }).snapshot.graph).nodes
      .find((node: { id: string }) => node.id === "agent") as
      | {
          id: string;
          position: { x: number; y: number };
          data: { agentConfig: { executor: { agentCli: string; modelId: string } } };
        }
      | undefined;
  const savedExecutor = () => savedNode()?.data.agentConfig.executor;
  const flushDraft = async () => {
    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await screen.findByRole("alertdialog", { name: "发布工作流" });
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "发布工作流" })).not.toBeInTheDocument());
  };
  return { backend, workflowId, queries, client, update, initialExecutor, savedNode, savedExecutor, flushDraft };
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

  it("restores the repaired model when undoing deletion of a stale-model node after late catalog arrival", async () => {
    const user = userEvent.setup();
    const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });

    const nodeCard = document.querySelector<HTMLElement>('.react-flow__node[data-id="agent"]');
    expect(nodeCard).not.toBeNull();
    await user.click(nodeCard!);

    const inspector = await waitFor(() => {
      const aside = document.querySelector<HTMLElement>("[data-workflow-inspector]");
      expect(aside).not.toBeNull();
      return aside!;
    });
    await user.click(within(inspector).getByRole("button", { name: "删除节点" }));
    await waitFor(() => expect(document.querySelector('.react-flow__node[data-id="agent"]')).toBeNull());
    expect(await screen.findByText("未保存")).toBeInTheDocument();

    act(() => {
      state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
    });

    await user.click(screen.getByRole("button", { name: "撤销" }));
    await waitFor(() => expect(document.querySelector('.react-flow__node[data-id="agent"]')).not.toBeNull());
    await waitFor(() =>
      expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0),
    );

    await state.flushDraft();
    expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });
  });

  it("repairs undo history when the user changed executor X to Y before late catalog arrival, and keeps Y on redo", async () => {
    const user = userEvent.setup();
    agentModelsCatalogOverride = {
      agents: [
        { agentRef: agent, label: "OpenCode", logo: null },
        { agentRef: "another-agent", label: "Other Agent", logo: null },
      ],
      agentModels: [],
      modelsByCli: new Map(),
      cliStatus: {
        [agent]: { isLoading: true, isError: false },
        "another-agent": { isLoading: true, isError: false },
      },
      isLoading: true,
      isError: false,
      refetch: vi.fn(),
    };
    try {
      const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
      const nodeCard = document.querySelector<HTMLElement>('.react-flow__node[data-id="agent"]');
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
      act(() => {
        state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
      });

      await state.flushDraft();
      expect(state.savedExecutor()).toEqual({
        agentCli: "another-agent",
        modelId: "deepseek/deepseek-v4-flash",
      });

      await user.click(screen.getByRole("button", { name: "撤销" }));
      await waitFor(() =>
        expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0),
      );
      await state.flushDraft();
      expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });

      await user.click(screen.getByRole("button", { name: "重做" }));
      await waitFor(() =>
        expect(screen.getAllByText(/another-agent · deepseek\/deepseek-v4-flash|Other Agent/).length).toBeGreaterThan(0),
      );
      await state.flushDraft();
      expect(state.savedExecutor()).toEqual({
        agentCli: "another-agent",
        modelId: "deepseek/deepseek-v4-flash",
      });
    } finally {
      agentModelsCatalogOverride = null;
    }
  }, 15_000);

  it("preserves intermediate Y when user toggled X -> Y -> X before late catalog arrival", async () => {
    const user = userEvent.setup();
    let now = Date.now();
    const nowSpy = vi.spyOn(Date, "now").mockImplementation(() => now);
    agentModelsCatalogOverride = {
      agents: [
        { agentRef: agent, label: "OpenCode", logo: null },
        { agentRef: "another-agent", label: "Other Agent", logo: null },
      ],
      agentModels: [],
      modelsByCli: new Map(),
      cliStatus: {
        [agent]: { isLoading: true, isError: false },
        "another-agent": { isLoading: true, isError: false },
      },
      isLoading: true,
      isError: false,
      refetch: vi.fn(),
    };
    try {
      const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
      const nodeCard = document.querySelector<HTMLElement>('.react-flow__node[data-id="agent"]');
      await user.click(nodeCard!);
      const inspector = await waitFor(() => {
        const aside = document.querySelector<HTMLElement>("[data-workflow-inspector]");
        expect(aside).not.toBeNull();
        return aside!;
      });

      // Step 1: X -> Y
      await user.click(within(inspector).getByRole("button", { name: "Agent 模型" }));
      await user.click(await screen.findByRole("option", { name: "Other Agent" }));
      // Advance past HISTORY_GROUP_WINDOW_MS (500ms) so the second toggle records its own frame.
      now += 1000;

      // Step 2: Y -> X (popover stays open after selectAgentCli so the model can be picked).
      await user.click(await screen.findByRole("option", { name: "OpenCode" }));
      await user.keyboard("{Escape}");

      agentModelsCatalogOverride = null;
      act(() => {
        state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
      });

      await waitFor(() =>
        expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0),
      );
      await state.flushDraft();
      expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });

      // Undo #1 must restore Y (not overwrite Y with repaired X).
      await user.click(screen.getByRole("button", { name: "撤销" }));
      await waitFor(() =>
        expect(screen.getAllByText(/another-agent · deepseek\/deepseek-v4-flash|Other Agent/).length).toBeGreaterThan(0),
      );
      await state.flushDraft();
      expect(state.savedExecutor()).toEqual({
        agentCli: "another-agent",
        modelId: "deepseek/deepseek-v4-flash",
      });

      // Undo #2 must restore repaired X (not stale X).
      await user.click(screen.getByRole("button", { name: "撤销" }));
      await waitFor(() =>
        expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0),
      );
      await state.flushDraft();
      expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });
    } finally {
      nowSpy.mockRestore();
      agentModelsCatalogOverride = null;
    }
  }, 15_000);

  it("keeps the repaired model across redo when late catalog arrives while edits are in the redo stack", async () => {
    const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
    fireEvent.click(screen.getByRole("button", { name: "Agent" }));
    expect(await screen.findByText("未保存")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "撤销" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "重做" })).toBeEnabled());

    act(() => {
      state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
    });
    await waitFor(() =>
      expect(screen.getAllByText(/official\/ora-space\.opencode · configured|Current/).length).toBeGreaterThan(0),
    );

    fireEvent.click(screen.getByRole("button", { name: "重做" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "撤销" })).toBeEnabled());
    expect(await screen.findByText("未保存")).toBeInTheDocument();

    await state.flushDraft();
    const savedNodes = JSON.parse(state.backend.getDraft({ workflowId: state.workflowId }).snapshot.graph).nodes as {
      id: string;
      data: { kind: string; agentConfig?: { executor: { agentCli: string; modelId: string } } };
    }[];
    expect(savedNodes.filter((node) => node.data.kind === "agent")).toHaveLength(2);
    expect(state.savedExecutor()).toEqual({ agentCli: agent, modelId: "configured" });
  });

  it("keeps the repaired model across drop, undo, and redo when late catalog arrives mid-drag", async () => {
    Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
      configurable: true,
      get: () => 240,
    });
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
      configurable: true,
      get: () => 80,
    });
    try {
      const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
      const nodeCard = document.querySelector<HTMLElement>('.react-flow__node[data-id="agent"]');
      expect(nodeCard).not.toBeNull();
      fireEvent.click(nodeCard!);
      const inspector = await waitFor(() => {
        const aside = document.querySelector<HTMLElement>("[data-workflow-inspector]");
        expect(aside).not.toBeNull();
        return aside!;
      });

      await act(async () => {
        nodeCard!.dispatchEvent(
          windowedMouseEvent("mousedown", { button: 0, clientX: 320, clientY: 100 }),
        );
      });
      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mousemove", { button: 0, clientX: 330, clientY: 110 }),
        );
      });
      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mousemove", { button: 0, clientX: 440, clientY: 180 }),
        );
      });

      // Catalog arrives while the drag transaction is open and live positions live on workflowRef.
      state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
      await waitFor(() =>
        expect(within(inspector).getByLabelText("Agent 模型")).toHaveTextContent(/Current|configured/),
      );

      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mouseup", { button: 0, clientX: 440, clientY: 180 }),
        );
      });

      expect(await screen.findByText("未保存")).toBeInTheDocument();
      await state.flushDraft();
      const moved = state.savedNode();
      expect(moved).toBeDefined();
      expect(moved!.position).not.toEqual({ x: 320, y: 0 });
      expect(moved!.data.agentConfig.executor).toEqual({ agentCli: agent, modelId: "configured" });

      fireEvent.click(screen.getByRole("button", { name: "撤销" }));
      await waitFor(() => expect(screen.getByRole("button", { name: "重做" })).toBeEnabled());
      await state.flushDraft();
      const undone = state.savedNode();
      expect(undone!.position).toEqual({ x: 320, y: 0 });
      expect(undone!.data.agentConfig.executor).toEqual({ agentCli: agent, modelId: "configured" });

      fireEvent.click(screen.getByRole("button", { name: "重做" }));
      await waitFor(() => expect(screen.getByRole("button", { name: "撤销" })).toBeEnabled());
      await state.flushDraft();
      const redone = state.savedNode();
      expect(redone!.position).toEqual(moved!.position);
      expect(redone!.data.agentConfig.executor).toEqual({ agentCli: agent, modelId: "configured" });
    } finally {
      delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth;
      delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight;
    }
  });

  it("keeps the draft clean when a mid-drag late catalog repair is followed by a drop back at the start", async () => {
    Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
      configurable: true,
      get: () => 240,
    });
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
      configurable: true,
      get: () => 80,
    });
    try {
      const state = await openEditor("deepseek/deepseek-v4-flash", { models: null });
      const nodeCard = document.querySelector<HTMLElement>('.react-flow__node[data-id="agent"]');
      expect(nodeCard).not.toBeNull();

      // Drag without opening the inspector: mounting its prompt editor emits one text change
      // that dirties the draft on its own, which would hide what this test pins down. The first
      // small move starts the gesture (nodeDragThreshold), fixing the drag's coordinate frame.
      await act(async () => {
        nodeCard!.dispatchEvent(
          windowedMouseEvent("mousedown", { button: 0, clientX: 320, clientY: 100 }),
        );
      });
      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mousemove", { button: 0, clientX: 330, clientY: 110 }),
        );
      });
      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mousemove", { button: 0, clientX: 440, clientY: 180 }),
        );
      });

      // Catalog arrives mid-drag; the repair must reach the drag-start snapshot too, or the drop
      // below compares a stale baseline against the repaired live graph and dirties the draft.
      state.queries.setQueryData(["workflow-catalog", "executors", tenantId], { items: catalog });
      await act(async () => {});
      await act(async () => {});

      // Drag back to the exact start and drop: nothing authored changed, so the draft stays clean.
      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mousemove", { button: 0, clientX: 330, clientY: 110 }),
        );
      });
      await act(async () => {
        window.dispatchEvent(
          windowedMouseEvent("mouseup", { button: 0, clientX: 330, clientY: 110 }),
        );
      });

      expect(screen.queryByText("未保存")).not.toBeInTheDocument();
      expect(state.update).not.toHaveBeenCalled();
      await state.flushDraft();
      const saved = state.savedNode();
      expect(saved!.position).toEqual({ x: 320, y: 0 });
      expect(saved!.data.agentConfig.executor).toEqual({ agentCli: agent, modelId: "configured" });
    } finally {
      delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth;
      delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight;
    }
  });
});
