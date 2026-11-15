import { useCallback, useEffect, useMemo, useState } from "react";

import { useTranslations } from "next-intl";

import { api } from "@/lib/api";
import type { Agent } from "@/lib/types";

// Built-in agent names, descriptions, and prompt-variable labels are seeded database values
// (builtinAgents in db/db.go and the retest agent in server/finding_retests.go).
// The database cannot hold per-locale values, and ON CONFLICT DO NOTHING leaves existing installs unchanged.
// Resolve labels by key from the builtinAgents namespace in web/messages/{ko,en,zh,es,ja}.json,
// falling back to the stored value if no translation exists. User-created agents always use their database values.

// Although retester is seeded with builtin=false (server FindingRetestAgentKey), its fixed system key
// makes it eligible for translation. Agents with builtin=true (goals/planner/mainagent/worker/auto/pentest)
// are already covered by agent.builtin and need no entry here.
const SYSTEM_SEEDED_AGENT_KEYS = new Set(["retester"]);

function isSystemSeeded(agent: { key: string; builtin?: boolean }): boolean {
  return !!agent.builtin || SYSTEM_SEEDED_AGENT_KEYS.has(agent.key);
}

// The system seeds reporter through seedReporterAgent in server/orchestration.go, but users can edit it.
// Translate values matching either the current English seed or the legacy Korean seed; preserve user edits.
const EDITABLE_SEEDS: Record<string, { name: string[]; description: string[] }> = {
  reporter: {
    name: ["Report writer", "보고서 작성"],
    description: [
      "Write detailed vulnerability reports: automatically triggered when a vulnerability is found, reads the evidence and execution trace, then writes a Markdown report to the vulnerability record.",
      "취약점 상세 보고서 작성: 취약점을 발견하면 자동으로 트리거되어, 증거와 실행 과정을 조회한 뒤 Markdown 보고서를 작성해 해당 취약점에 기록합니다.",
    ],
  },
};

// useAgentLabels provides localized names, descriptions, and prompt-variable labels for built-in agents.
// All display sites obtain labels through these helpers.
export function useAgentLabels() {
  const t = useTranslations("builtinAgents");

  // Callers use these helpers as useMemo/useCallback dependencies for conversation groups and filters.
  // Recreate the bundle only when the translation function changes, avoiding recomputation on every render.
  return useMemo(() => {
    // Return the translated name for built-in agents, or the stored agent.name otherwise.
    const name = (agent: { key: string; name: string; builtin?: boolean }): string => {
      const translatable = isSystemSeeded(agent) || EDITABLE_SEEDS[agent.key]?.name.includes(agent.name);
      if (translatable && t.has(`${agent.key}.name`)) return t(`${agent.key}.name`);
      return agent.name;
    };

    // Return the translated description for built-in agents, otherwise the stored description or an empty string.
    // Callers continue to supply fallback text for empty descriptions.
    const description = (agent: { key: string; description?: string; builtin?: boolean }): string => {
      const translatable =
        isSystemSeeded(agent) || EDITABLE_SEEDS[agent.key]?.description.includes(agent.description ?? "");
      if (translatable && t.has(`${agent.key}.description`)) return t(`${agent.key}.description`);
      return agent.description ?? "";
    };

    // nameByKey handles cases with only a key, such as conversation grouping.
    // Use the supplied agent lookup to translate a match, otherwise return the key.
    const nameByKey = (
      key: string,
      lookup: (key: string) => { key: string; name: string; builtin?: boolean } | undefined,
    ): string => {
      const agent = lookup(key);
      // biome-ignore lint/nursery/useNullishCoalescing: || intentionally falls back to the key for agents with an empty name, preserving existing behavior.
      return (agent && name(agent)) || key;
    };

    // Translate built-in prompt-variable labels, such as the overall task goal for goals.
    // If no translation exists, retain the database label passed as fallback.
    const varLabel = (agentKey: string, builtin: boolean, varName: string, fallback: string): string => {
      if ((builtin || SYSTEM_SEEDED_AGENT_KEYS.has(agentKey)) && t.has(`${agentKey}.vars.${varName}`)) {
        return t(`${agentKey}.vars.${varName}`);
      }
      return fallback;
    };

    return { name, description, nameByKey, varLabel };
  }, [t]);
}

// Several screens, including approval records, use the agent list only to map keys to display names.
// Load once and share at module scope; retry on the next call after a failure.
let agentListPromise: Promise<Agent[]> | null = null;
function loadAgentList(): Promise<Agent[]> {
  if (!agentListPromise) {
    agentListPromise = api.agents().catch((err) => {
      agentListPromise = null;
      throw err;
    });
  }
  return agentListPromise;
}

// useAgentNameByKey provides labels for screens holding only an agent key, such as conv_agent_key
// in approval/interception records. Use localized built-in names and stored custom-agent names;
// fall back to the key when the list is unavailable or contains no match.
export function useAgentNameByKey(): (key: string) => string {
  const labels = useAgentLabels();
  const [agents, setAgents] = useState<Map<string, Agent>>(() => new Map());
  useEffect(() => {
    let alive = true;
    loadAgentList()
      .then((list) => {
        if (alive) setAgents(new Map(list.map((a) => [a.key, a])));
      })
      .catch(() => {
        // If loading fails, display the key and retry on the next mount.
      });
    return () => {
      alive = false;
    };
  }, []);
  return useCallback((key: string) => labels.nameByKey(key, (k) => agents.get(k)), [labels, agents]);
}
