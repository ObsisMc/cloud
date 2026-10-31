import { describe, expect, it } from "vitest";
import { ContractError } from "../contracts";
import { localWorkflowContracts, memoryWorkflowStorage } from "../test/workflow-server";
import { LocalWorkflowBackend } from "./local-workflow-backend";
import {
  forgetPendingDraft,
  pendingDrafts,
  rememberPendingDraft,
  replayPendingDrafts,
  type PendingDraftStorage,
} from "./pending-drafts";

const graph = '{"nodes":[],"edges":[]}';

function memory(): PendingDraftStorage & { value: string | null } {
  const storage = {
    value: null as string | null,
    load: () => storage.value,
    save: (serialized: string | null) => {
      storage.value = serialized;
    },
  };
  return storage;
}

describe("pending drafts", () => {
  it("keeps one draft per workflow and tenant, the latest winning", () => {
    const storage = memory();
    rememberPendingDraft("t1", { workflowId: "w1", graph: "old", name: "a" }, storage);
    rememberPendingDraft("t1", { workflowId: "w1", graph: "new", name: "a" }, storage);
    rememberPendingDraft("t2", { workflowId: "w9", graph: "other", name: "b" }, storage);

    expect(pendingDrafts("t1", storage)).toEqual([{ workflowId: "w1", graph: "new", name: "a" }]);
    forgetPendingDraft("t1", "w1", storage);
    expect(pendingDrafts("t1", storage)).toEqual([]);
    forgetPendingDraft("t2", "w9", storage);
    expect(storage.value).toBeNull();
  });

  it("writes the drafts and renames the workflow, then forgets them", async () => {
    const storage = memory();
    const server = new LocalWorkflowBackend({ storage: memoryWorkflowStorage() });
    const { workflow } = server.create({ name: "旧名", graph: null });
    rememberPendingDraft("t1", { workflowId: workflow.id, graph, name: "新名" }, storage);

    expect(await replayPendingDrafts("t1", localWorkflowContracts(server), storage)).toBe(0);

    expect(server.getDraft({ workflowId: workflow.id }).snapshot.graph).toBe(graph);
    expect(server.get({ workflowId: workflow.id }).workflow.name).toBe("新名");
    expect(pendingDrafts("t1", storage)).toEqual([]);
  });

  it("drops a deleted workflow's draft and keeps one the server could not take", async () => {
    const storage = memory();
    const server = new LocalWorkflowBackend({ storage: memoryWorkflowStorage() });
    const { workflow } = server.create({ name: "留着", graph: null });
    rememberPendingDraft("t1", { workflowId: "gone", graph, name: "" }, storage);
    rememberPendingDraft("t1", { workflowId: workflow.id, graph, name: "留着" }, storage);
    const contracts = {
      ...localWorkflowContracts(server),
      updateDraft: async ({ workflowId }: { workflowId: string }) => {
        if (workflowId === "gone") throw new ContractError("workflow_not_found");
        throw new ContractError("workflow_store_unavailable");
      },
    };

    expect(await replayPendingDrafts("t1", contracts, storage)).toBe(1);
    expect(pendingDrafts("t1", storage).map((d) => d.workflowId)).toEqual([workflow.id]);
  });

  it("keeps the graph when the title is refused", async () => {
    const storage = memory();
    const server = new LocalWorkflowBackend({ storage: memoryWorkflowStorage() });
    server.create({ name: "占用", graph: null });
    const { workflow } = server.create({ name: "原名", graph: null });
    rememberPendingDraft("t1", { workflowId: workflow.id, graph, name: "占用" }, storage);

    expect(await replayPendingDrafts("t1", localWorkflowContracts(server), storage)).toBe(0);
    expect(server.getDraft({ workflowId: workflow.id }).snapshot.graph).toBe(graph);
    expect(server.get({ workflowId: workflow.id }).workflow.name).toBe("原名");
  });
});
