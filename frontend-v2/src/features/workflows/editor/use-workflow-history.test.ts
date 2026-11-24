import { act, renderHook } from "@testing-library/react";
import type { Edge } from "@xyflow/react";
import { describe, expect, it, vi } from "vitest";
import {
  createMockWorkflowCapabilities,
  type DemoWorkflow,
} from "@/lib/workflows/mock";
import { useWorkflowHistory } from "./use-workflow-history";
import { captureWorkflowHistorySnapshot } from "./workflow-history";

/** Builds a minimal workflow fixture with one Agent node for hook-level history tests. */
function makeWorkflow(
  modelId: string,
  position = { x: 0, y: 0 },
): DemoWorkflow {
  return {
    id: "wf-1",
    name: "Hook Test",
    description: "",
    updatedAt: "2026-01-01T00:00:00.000Z",
    viewport: { x: 0, y: 0, zoom: 1 },
    nodes: [
      {
        id: "agent",
        type: "workflow",
        position,
        data: {
          kind: "agent",
          title: "Agent",
          description: "",
          agentConfig: {
            ...createMockWorkflowCapabilities("zh-CN").defaultAgentConfig,
            executor: { agentCli: "official/ora-space.opencode", modelId },
            prompt: "",
          },
        },
      },
    ],
    edges: [] as Edge[],
  };
}

function modelOf(workflowOrSnapshot: { nodes: DemoWorkflow["nodes"] }): string {
  return workflowOrSnapshot.nodes[0]!.data.agentConfig!.executor.modelId;
}

function positionOf(workflowOrSnapshot: { nodes: DemoWorkflow["nodes"] }): {
  x: number;
  y: number;
} {
  return workflowOrSnapshot.nodes[0]!.position;
}

describe("useWorkflowHistory.rewriteSnapshots", () => {
  it("transforms currentSnapshot along with past and future frames", () => {
    const onRestore = vi.fn();
    const { result } = renderHook(() => useWorkflowHistory(onRestore));

    const initial = makeWorkflow("stale");
    const edited = makeWorkflow("stale", { x: 80, y: 40 });
    const initialSnap = captureWorkflowHistorySnapshot(initial);
    const editedSnap = captureWorkflowHistorySnapshot(edited);

    act(() => {
      result.current.reset(initial);
      result.current.record(initialSnap, editedSnap, "node.move");
      result.current.undo(edited);
    });

    // After undo, currentSnapshot is initialSnap and future holds editedSnap.
    expect(modelOf(result.current.currentSnapshot!)).toBe("stale");

    act(() => {
      result.current.rewriteSnapshots((snapshot) => ({
        ...snapshot,
        nodes: snapshot.nodes.map((node) => ({
          ...node,
          data: {
            ...node.data,
            agentConfig: {
              ...node.data.agentConfig!,
              executor: {
                agentCli: "official/ora-space.opencode",
                modelId: "configured",
              },
            },
          },
        })),
      }));
    });

    expect(modelOf(result.current.currentSnapshot!)).toBe("configured");
    expect(modelOf(result.current.future[0]!.snapshot)).toBe("configured");
  });

  it("transforms the pending transaction before snapshot so commit and undo keep the repair", () => {
    const onRestore = vi.fn();
    const { result } = renderHook(() => useWorkflowHistory(onRestore));

    const initial = makeWorkflow("stale", { x: 10, y: 20 });
    const initialSnap = captureWorkflowHistorySnapshot(initial);
    const movedRepaired = makeWorkflow("configured", { x: 140, y: 90 });
    const movedRepairedSnap = captureWorkflowHistorySnapshot(movedRepaired);

    act(() => {
      result.current.reset(initial);
      result.current.beginTransaction(initialSnap, "node.move");
      result.current.rewriteSnapshots((snapshot) => ({
        ...snapshot,
        nodes: snapshot.nodes.map((node) => ({
          ...node,
          data: {
            ...node.data,
            agentConfig: {
              ...node.data.agentConfig!,
              executor: {
                agentCli: "official/ora-space.opencode",
                modelId: "configured",
              },
            },
          },
        })),
      }));
      result.current.commitTransaction(movedRepairedSnap);
    });

    expect(result.current.canUndo).toBe(true);
    expect(modelOf(result.current.past[0]!.snapshot)).toBe("configured");

    act(() => {
      result.current.undo(movedRepaired);
    });

    expect(onRestore).toHaveBeenCalledTimes(1);
    const restored = onRestore.mock.calls[0]![0];
    expect(positionOf(restored)).toEqual({ x: 10, y: 20 });
    expect(modelOf(restored)).toBe("configured");
  });
});
