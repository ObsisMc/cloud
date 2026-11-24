import { describe, expect, it } from "vitest";
import { ContractError } from "@/lib/workflows/contracts";
import { workflowI18n } from "@/lib/workflows/host/i18n";
import { describePublishFailure } from "./workflow-publish-error";

const t = workflowI18n.t;
const titles: Record<string, string> = { out: "汇总输出" };
const title = (id: string) => titles[id];

describe("describePublishFailure", () => {
  it("names the refused node by title, the field, and the server's reason", () => {
    const failure = describePublishFailure(
      new ContractError("workflow_run_graph_parse", {
        nodeId: "out",
        field: "selector",
        reason: "Loop loop-1 publishes only its named outputs (result); loop-1.output is not one",
      }),
      title,
      t,
    );
    expect(failure).toEqual({
      message: "发布被拒绝：节点「汇总输出」的变量引用没有通过发布校验，草稿已保存。",
      detail: "Loop loop-1 publishes only its named outputs (result); loop-1.output is not one",
    });
  });

  it("reads the server's data paths, indices included, as the field the author edits", () => {
    const message = (field: string) =>
      describePublishFailure(
        new ContractError("workflow_run_graph_parse", { nodeId: "out", field, reason: "x" }),
        title,
        t,
      ).message;
    expect(message("data.aggregatorConfig")).toBe(
      "发布被拒绝：节点「汇总输出」的聚合变量没有通过发布校验，草稿已保存。",
    );
    expect(message("data.outputs[1].variableSelector")).toBe(
      "发布被拒绝：节点「汇总输出」的输出结果没有通过发布校验，草稿已保存。",
    );
    expect(message("iterationConfig.collectSelector")).toBe(
      "发布被拒绝：节点「汇总输出」的收集变量没有通过发布校验，草稿已保存。",
    );
    // Templates are named by where they were written, not as a generic "template".
    expect(message("agentConfig.prompt")).toBe(
      "发布被拒绝：节点「汇总输出」的提示词变量没有通过发布校验，草稿已保存。",
    );
    expect(message("data.agentConfig.executor")).toBe(
      "发布被拒绝：节点「汇总输出」的Agent 模型没有通过发布校验，草稿已保存。",
    );
    expect(message("data.agentConfig.executor.agentCli")).toBe(
      "发布被拒绝：节点「汇总输出」的Agent 模型没有通过发布校验，草稿已保存。",
    );
    expect(message("data.agentConfig.executor.modelId")).toBe(
      "发布被拒绝：节点「汇总输出」的Agent 模型没有通过发布校验，草稿已保存。",
    );
    expect(message("cases")).toBe(
      "发布被拒绝：节点「汇总输出」的分支条件没有通过发布校验，草稿已保存。",
    );
    expect(message("loopConfig.until")).toBe(
      "发布被拒绝：节点「汇总输出」的结束条件没有通过发布校验，草稿已保存。",
    );
    expect(message("loopConfig.variables[0].initial")).toBe(
      "发布被拒绝：节点「汇总输出」的循环变量初始值没有通过发布校验，草稿已保存。",
    );
    expect(message("loopConfig.outputs[0].variableSelector")).toBe(
      "发布被拒绝：节点「汇总输出」的循环输出没有通过发布校验，草稿已保存。",
    );
  });

  it("falls back to the node id and the raw field the editor has no words for", () => {
    const failure = describePublishFailure(
      new ContractError("workflow_run_graph_parse", { nodeId: "gone", field: "data.kind", reason: "x" }),
      title,
      t,
    );
    expect(failure.message).toBe("发布被拒绝：节点「gone」没有通过发布校验（data.kind），草稿已保存。");
  });

  it("names the node without a field when the server gives none", () => {
    const failure = describePublishFailure(
      new ContractError("workflow_run_graph_parse", { nodeId: "out", reason: "x" }),
      title,
      t,
    );
    expect(failure.message).toBe("发布被拒绝：节点「汇总输出」没有通过发布校验，草稿已保存。");
  });

  it("says the graph was rejected when no node is known", () => {
    expect(
      describePublishFailure(new ContractError("workflow_run_graph_parse", {}), title, t),
    ).toEqual({ message: "发布被拒绝：工作流图没有通过发布校验，草稿已保存。", detail: null });
  });

  it("keeps other refusals' own message under a publish prefix", () => {
    expect(
      describePublishFailure(new ContractError("workflow_version_already_exists"), title, t),
    ).toEqual({ message: "发布失败：该工作流版本已存在。", detail: null });
    expect(describePublishFailure(new Error("network"), title, t).message).toMatch(/^发布失败：/);
  });
});
