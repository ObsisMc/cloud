import type { TFunction } from "i18next";
import { ContractError } from "../contracts";

/** Localizes a refused operation by its code using the active i18next instance's loaded bundles. */
export function localizeContractError(error: unknown, t: TFunction): string {
  if (error instanceof ContractError) {
    if (error.code === "workflow_skill_not_found") {
      const skillId = typeof error.params.skillId === "string" ? error.params.skillId.trim() : "";
      const nodeTitle =
        (typeof error.params.nodeTitle === "string" && error.params.nodeTitle.trim()) ||
        (typeof error.params.nodeId === "string" && error.params.nodeId.trim()) ||
        "";
      if (skillId && nodeTitle) {
        const detail = t("errors.workflow_skill_not_found_detail", {
          nodeTitle,
          skillId,
          defaultValue: "",
        });
        if (detail && detail !== "errors.workflow_skill_not_found_detail") {
          return detail;
        }
      }
    }
    const key = `errors.${error.code}`;
    const localized = t(key, { ...error.params, defaultValue: "" });
    if (localized && localized !== key) {
      return localized;
    }
  }
  return t("errors.unknown");
}
