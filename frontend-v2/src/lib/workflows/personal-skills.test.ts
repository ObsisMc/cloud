import { describe, expect, it } from "vitest";
import { graphHasEnabledPersonalSkills, isPersonalSkillId } from "./personal-skills";

describe("personal-skills", () => {
  it("distinguishes personal Skill names from marketplace plugin IDs", () => {
    expect(isPersonalSkillId("review-checklist")).toBe(true);
    expect(isPersonalSkillId("  code_review  ")).toBe(true);
    expect(isPersonalSkillId("")).toBe(false);
    expect(isPersonalSkillId("   ")).toBe(false);
    expect(isPersonalSkillId("official/mattpocock.skills")).toBe(false);
  });

  it("detects enabled personal Skills across root and container Agent nodes", () => {
    const enabledPersonal = JSON.stringify({
      nodes: [
        { id: "start", data: { kind: "start" } },
        {
          id: "writer",
          data: {
            kind: "agent",
            agentConfig: {
              prompt: "Draft",
              skills: [
                { skillId: "official/mattpocock.skills", enabled: true },
                { skillId: "personal-checklist", enabled: true },
              ],
            },
          },
        },
      ],
      edges: [{ source: "start", target: "writer" }],
    });
    expect(graphHasEnabledPersonalSkills(enabledPersonal)).toBe(true);

    const disabledOrMarketOnly = JSON.stringify({
      nodes: [
        { id: "start", data: { kind: "start" } },
        {
          id: "writer",
          data: {
            kind: "agent",
            agentConfig: {
              prompt: "Draft",
              skills: [
                { skillId: "official/mattpocock.skills", enabled: true },
                { skillId: "personal-checklist", enabled: false },
              ],
            },
          },
        },
      ],
      edges: [{ source: "start", target: "writer" }],
    });
    expect(graphHasEnabledPersonalSkills(disabledOrMarketOnly)).toBe(false);
    expect(graphHasEnabledPersonalSkills(undefined)).toBe(false);
    expect(graphHasEnabledPersonalSkills("not json")).toBe(false);
  });

  it("treats bindings omitting enabled as enabled and ignores malformed spare node skill configs", () => {
    const spareWithInvalidAndOmittedEnabled = JSON.stringify({
      nodes: [
        { id: "start", data: { kind: "start" } },
        {
          id: "spare-object-skills",
          data: {
            kind: "agent",
            agentConfig: {
              prompt: "Draft",
              skills: {},
            },
          },
        },
        {
          id: "spare-malformed-entries",
          data: {
            kind: "agent",
            agentConfig: {
              prompt: "Draft",
              skills: [null, {}, { skillId: 123, enabled: true }, { skillId: "personal-disabled", enabled: false }],
            },
          },
        },
      ],
      edges: [],
    });
    expect(graphHasEnabledPersonalSkills(spareWithInvalidAndOmittedEnabled)).toBe(false);

    const omittedEnabled = JSON.stringify({
      nodes: [
        {
          id: "spare-object-skills",
          data: {
            kind: "agent",
            agentConfig: {
              prompt: "Draft",
              skills: {},
            },
          },
        },
        {
          id: "writer",
          data: {
            kind: "agent",
            agentConfig: {
              prompt: "Draft",
              skills: [{ skillId: "implicit-enabled-skill" }],
            },
          },
        },
      ],
      edges: [],
    });
    expect(graphHasEnabledPersonalSkills(omittedEnabled)).toBe(true);
  });
});
