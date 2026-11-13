import type { PluginCatalogEntry } from '@/api/generated.schemas'
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { demoMockEnabled } from './flags'

const EMPTY_SELECTION: string[] = []

interface DemoPluginSelections {
  byScope: Record<string, string[]>
  setSelected: (scope: string, plugin: string, selected: boolean) => void
  reset: () => void
}

/**
 * Browser-local install state for the v2 demo recording. It never represents a request to the
 * server: every value lives in localStorage, scoped by user, tenant and space so one person's demo
 * cannot change another workspace or login.
 */
export const useDemoPluginSelections = create<DemoPluginSelections>()(
  persist(
    (set) => ({
      byScope: {},
      setSelected: (scope, plugin, selected) =>
        set((state) => {
          const current = state.byScope[scope] ?? EMPTY_SELECTION
          const next = selected
            ? current.includes(plugin)
              ? current
              : [...current, plugin]
            : current.filter((item) => item !== plugin)
          return { byScope: { ...state.byScope, [scope]: next } }
        }),
      reset: () => set({ byScope: {} }),
    }),
    {
      name: 'ora-v2-plugin-demo:v1',
      version: 1,
      storage: createJSONStorage(() => localStorage),
    },
  ),
)

export function demoSelectionFor(scope: string) {
  return (state: DemoPluginSelections) =>
    state.byScope[scope] ?? EMPTY_SELECTION
}

const DEMO_INDEXED_AT = '2026-11-02T00:00:00Z'
const DEMO_SOURCE_URL = '本地'

function demoEntry(
  identifier: string,
  kind: 'mcp' | 'hook',
  title: string,
  description: string,
): PluginCatalogEntry {
  return {
    id: `demo/${identifier}`,
    sourceNamespace: 'demo',
    identifier,
    title,
    kind,
    version: '1.0.0',
    description,
    homepage: null,
    license: 'MIT',
    logo: null,
    url: null,
    sha256: null,
    targets: null,
    packMembers: null,
    readme: `# ${title}\n\n${description}`,
    marketplaceVisible: true,
    sourceUrl: DEMO_SOURCE_URL,
    indexedAt: DEMO_INDEXED_AT,
  }
}

export const demoMcpEntry = demoEntry(
  'sample-mcp',
  'mcp',
  '示例 MCP 工具',
  '演示用的 MCP 示例，安装只记录浏览器本地状态，不会连接外部服务。',
)

export const demoHookEntry = demoEntry(
  'sample-hooks',
  'hook',
  '示例 Hooks',
  '生命周期 Hook 的演示安装只记录浏览器本地状态，不下载或执行脚本。',
)

/**
 * In the demo recording the catalog keeps a sample MCP and Hook so the page can show their
 * lifecycle. A real listing from a synced marketplace takes precedence, so this never hides an
 * actual package. Outside the demo (`VITE_DEMO_MOCK` unset) the catalog is returned unchanged.
 */
export function withDemoCatalog(catalog: PluginCatalogEntry[]): PluginCatalogEntry[] {
  if (!demoMockEnabled()) return catalog
  return [
    ...catalog,
    ...(catalog.some((entry) => entry.kind === 'mcp') ? [] : [demoMcpEntry]),
    ...(catalog.some((entry) => entry.kind === 'hook') ? [] : [demoHookEntry]),
  ]
}

/**
 * Whether an install for this kind is a browser-only demo action. Agents (market and
 * runtime-preinstalled), Hooks and MCP have no server-backed package in v2; recording their
 * lifecycle is a demo concern, so only the demo mock installs them. Skills and workflows always
 * install through the server and are never demo installs.
 */
export function demoInstallable(entry: Pick<PluginCatalogEntry, 'kind'>): boolean {
  return (
    demoMockEnabled() &&
    (entry.kind === 'agent' || entry.kind === 'hook' || entry.kind === 'mcp')
  )
}

/** A 3–5 second pause so the demo install feels like the real ones; none outside the demo. */
export function demoInstallDelayMs(): number {
  return demoMockEnabled() ? 3000 + Math.random() * 2000 : 0
}

export function waitForDemoInstallDelay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
