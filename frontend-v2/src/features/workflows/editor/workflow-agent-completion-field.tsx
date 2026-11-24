import { useTranslation } from "react-i18next";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Switch,
} from "@/lib/workflows/host/ui";
import type { WorkflowAgentConfig } from "@/lib/workflows/mock";
import { InspectorField } from "./workflow-node-details";

/**
 * Chooses how an interactive node decides its settled turn is complete (desktop
 * `workflow-agent-completion-field.tsx`). An absent policy reads as `end_turn`, the value new
 * interactive nodes are written with.
 */
export function WorkflowAgentCompletionField({
  config,
  onChange,
}: {
  config: WorkflowAgentConfig;
  onChange: (config: WorkflowAgentConfig) => void;
}) {
  const { t } = useTranslation();
  const value = config.completionSignal === "agent" ? "agent" : "end_turn";

  return (
    <>
      <InspectorField
        label={t("settings.workflow.field.completionSignal")}
        htmlFor="workflow-agent-completion-signal"
      >
        <Select
          value={value}
          onValueChange={(completionSignal) => {
            if (completionSignal === "agent") {
              onChange({ ...config, schemaVersion: 4, completionSignal: "agent" });
            } else if (completionSignal === "end_turn") {
              // Auto-accept only exists for the agent policy.
              const { autoAcceptAgentCompletion: _dropped, ...rest } = config;
              onChange({ ...rest, schemaVersion: 4, completionSignal: "end_turn" });
            }
          }}
        >
          <SelectTrigger
            id="workflow-agent-completion-signal"
            className="w-full"
            aria-describedby="workflow-agent-completion-signal-hint"
          >
            <SelectValue>
              {t(`settings.workflow.completionSignal.${value}`)}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="end_turn">
              {t("settings.workflow.completionSignal.end_turn")}
            </SelectItem>
            <SelectItem value="agent">
              {t("settings.workflow.completionSignal.agent")}
            </SelectItem>
          </SelectContent>
        </Select>
        <p
          id="workflow-agent-completion-signal-hint"
          className="text-[10px] leading-relaxed text-muted-foreground"
        >
          {t("settings.workflow.completionSignal.hint")}
        </p>
      </InspectorField>
      {value === "agent" && (
        <div className="space-y-1.5">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0 space-y-0.5">
              <p className="text-[11px] font-medium">
                {t("settings.workflow.field.autoAcceptAgentCompletion")}
              </p>
              <p className="text-[10px] leading-relaxed text-muted-foreground">
                {t("settings.workflow.autoAcceptAgentCompletion.hint")}
              </p>
            </div>
            <Switch
              id="workflow-agent-auto-accept-completion"
              aria-label={t("settings.workflow.field.autoAcceptAgentCompletion")}
              className="shrink-0 data-checked:bg-brand hover:data-checked:bg-brand/90"
              checked={config.autoAcceptAgentCompletion ?? false}
              onCheckedChange={(autoAcceptAgentCompletion) =>
                onChange({
                  ...config,
                  schemaVersion: 4,
                  completionSignal: "agent",
                  autoAcceptAgentCompletion,
                })
              }
            />
          </div>
        </div>
      )}
    </>
  );
}
