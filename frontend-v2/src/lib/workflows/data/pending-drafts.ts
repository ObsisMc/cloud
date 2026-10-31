import { ContractError } from "../contracts";
import type { WorkflowContracts } from "./contracts-client";

const STORAGE_KEY = "ora.v2.workflows.pending-drafts";

/** A draft the page was still writing when it went away, kept until the server has it. */
export interface PendingDraft {
  workflowId: string;
  graph: string;
  /** The title as edited, which the same autosave renames the workflow to. */
  name: string;
}

/** Where pending drafts live between visits: synchronous, so a closing page can still write. */
export interface PendingDraftStorage {
  load(): string | null;
  save(serialized: string | null): void;
}

export const browserPendingDraftStorage: PendingDraftStorage = {
  load() {
    try {
      return window.localStorage.getItem(STORAGE_KEY);
    } catch {
      return null;
    }
  },
  save(serialized) {
    try {
      if (serialized === null) window.localStorage.removeItem(STORAGE_KEY);
      else window.localStorage.setItem(STORAGE_KEY, serialized);
    } catch {
      // Without storage only the keepalive request carries the last edits.
    }
  },
};

type Stored = Record<string, Record<string, PendingDraft>>;

function read(storage: PendingDraftStorage): Stored {
  try {
    const parsed = JSON.parse(storage.load() ?? "{}") as unknown;
    return parsed !== null && typeof parsed === "object" ? (parsed as Stored) : {};
  } catch {
    return {};
  }
}

function write(storage: PendingDraftStorage, stored: Stored): void {
  const tenants = Object.fromEntries(
    Object.entries(stored).filter(([, drafts]) => Object.keys(drafts).length > 0),
  );
  storage.save(Object.keys(tenants).length === 0 ? null : JSON.stringify(tenants));
}

/** Remembers `draft` as the tenant's latest unsaved edit of that workflow. */
export function rememberPendingDraft(
  tenantId: string,
  draft: PendingDraft,
  storage: PendingDraftStorage = browserPendingDraftStorage,
): void {
  const stored = read(storage);
  stored[tenantId] = { ...stored[tenantId], [draft.workflowId]: draft };
  write(storage, stored);
}

/** Forgets the workflow's pending draft once a newer write has reached the server. */
export function forgetPendingDraft(
  tenantId: string,
  workflowId: string,
  storage: PendingDraftStorage = browserPendingDraftStorage,
): void {
  const stored = read(storage);
  if (stored[tenantId]?.[workflowId] === undefined) return;
  const { [workflowId]: _forgotten, ...rest } = stored[tenantId];
  stored[tenantId] = rest;
  write(storage, stored);
}

export function pendingDrafts(
  tenantId: string,
  storage: PendingDraftStorage = browserPendingDraftStorage,
): PendingDraft[] {
  return Object.values(read(storage)[tenantId] ?? {});
}

/**
 * Writes every draft the tenant's page left unsaved, before the editor reads the library, so a
 * reload shows the last edits. A workflow deleted meanwhile drops its draft; any other refusal
 * keeps it for the next visit, unless a later autosave of the workflow succeeds first. Returns
 * how many drafts are still waiting.
 */
export async function replayPendingDrafts(
  tenantId: string,
  contracts: WorkflowContracts,
  storage: PendingDraftStorage = browserPendingDraftStorage,
): Promise<number> {
  let waiting = 0;
  for (const draft of pendingDrafts(tenantId, storage)) {
    try {
      await contracts.updateDraft({ workflowId: draft.workflowId, graph: draft.graph });
    } catch (error) {
      if (error instanceof ContractError && error.code === "workflow_not_found") {
        forgetPendingDraft(tenantId, draft.workflowId, storage);
        continue;
      }
      waiting += 1;
      continue;
    }
    // The graph is what matters; a title the server refuses (say, taken meanwhile) stays as it was.
    await renameIfEdited(contracts, draft).catch(() => undefined);
    forgetPendingDraft(tenantId, draft.workflowId, storage);
  }
  return waiting;
}

async function renameIfEdited(contracts: WorkflowContracts, draft: PendingDraft): Promise<void> {
  const name = draft.name.trim();
  if (name === "") return;
  const { workflow } = await contracts.get({ workflowId: draft.workflowId });
  if (workflow.name !== name) await contracts.update({ workflowId: draft.workflowId, name });
}
