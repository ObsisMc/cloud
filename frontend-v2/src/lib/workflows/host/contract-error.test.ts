import { describe, expect, it } from "vitest";
import { workflowRunTranslations } from "@/features/chat/workflow-run/translations";
import { ContractError } from "../contracts";
import { createWorkflowI18n } from "./i18n";
import { localizeContractError } from "./contract-error";

describe("localizeContractError", () => {
  const i18n = createWorkflowI18n(["zh-CN", "en-US"], workflowRunTranslations);
  const t = i18n.getFixedT("zh-CN");
  const tEn = i18n.getFixedT("en-US");

  it("formats workflow_skill_not_found with nodeTitle and skillId", () => {
    const err = new ContractError("workflow_skill_not_found", {
      nodeId: "agent-review",
      nodeTitle: "代码审查",
      skillId: "team-secret-skill",
    });
    expect(localizeContractError(err, t)).toBe(
      "节点「代码审查」需要的技能 team-secret-skill 不可用，请先在个人技能页导入同名技能或确认市场技能已下发到项目后重试。",
    );
    expect(localizeContractError(err, tEn)).toContain("代码审查");
    expect(localizeContractError(err, tEn)).toContain("team-secret-skill");
  });

  it("falls back to nodeId when nodeTitle is blank", () => {
    const err = new ContractError("workflow_skill_not_found", {
      nodeId: "agent-loop-body",
      nodeTitle: "   ",
      skillId: "official/ora-space.skill-pack",
    });
    expect(localizeContractError(err, t)).toBe(
      "节点「agent-loop-body」需要的技能 official/ora-space.skill-pack 不可用，请先在个人技能页导入同名技能或确认市场技能已下发到项目后重试。",
    );
  });

  it("falls back to generic workflow_skill_not_found when params are absent", () => {
    const err = new ContractError("workflow_skill_not_found", {});
    expect(localizeContractError(err, t)).toBe(
      "该工作流需要的技能不可用，请先在个人技能页导入同名技能或确认市场技能已下发到项目后重试。",
    );
  });

  it("localizes run-bundle errors such as workflow_role_not_found", () => {
    expect(localizeContractError(new ContractError("workflow_role_not_found", {}), t)).toBe(
      "工作流引用的角色在工作区角色目录里不存在，请先在设置的角色页新建同名角色再启动。",
    );
  });

  it("returns unknown error message for unrecognized codes", () => {
    expect(localizeContractError(new ContractError("unmapped_code", {}), t)).toBe(
      t("errors.unknown"),
    );
  });
});
