import {
  agentConfigNeedsSchemaVersion4,
  type WorkflowAgentConfig,
} from "@/lib/workflows/mock";

/**
 * Turns interactive mode on or off. A node turned interactive without a policy is written with
 * `completionSignal: "end_turn"` and `schemaVersion: 4`, the default circus#1242 set for v2. An
 * explicit choice already on the config stays. Turning the mode off drops the policy and its auto-accept, which only exist
 * for an interactive node, and returns the node to version 3 when nothing else needs version 4.
 */
export function withInteractive(
  config: WorkflowAgentConfig,
  interactive: boolean,
): WorkflowAgentConfig {
  if (interactive) {
    return config.completionSignal == null
      ? { ...config, interactive, schemaVersion: 4, completionSignal: "end_turn" }
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
