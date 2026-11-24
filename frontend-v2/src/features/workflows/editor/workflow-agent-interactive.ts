import {
  agentConfigNeedsSchemaVersion4,
  type WorkflowAgentConfig,
} from "@/lib/workflows/mock";

/**
 * Turns interactive mode on or off. A node turned interactive without a policy is written with
 * `completionSignal: "agent"` and `schemaVersion: 4`. A choice already on the config stays, so a
 * saved `end_turn` is never rewritten. Turning the mode off drops the policy and its auto-accept, which only exist
 * for an interactive node, and returns the node to version 3 when nothing else needs version 4.
 */
export function withInteractive(
  config: WorkflowAgentConfig,
  interactive: boolean,
): WorkflowAgentConfig {
  if (interactive) {
    return config.completionSignal == null
      ? { ...config, interactive, schemaVersion: 4, completionSignal: "agent" }
      : { ...config, interactive };
  }
  const {
    completionSignal: _signal,
    autoAcceptAgentCompletion: _autoAccept,
    ...rest
  } = config;
  return {
    ...rest,
    interactive,
    schemaVersion: agentConfigNeedsSchemaVersion4(rest) ? 4 : 3,
  };
}
