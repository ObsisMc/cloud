import { useQuery } from '@tanstack/react-query'
import type { SpacePlugin } from '@/api/generated.schemas'
import { getSkillProgress, type SkillProgress } from '../market-api/generated'
import { pluginKey } from './marketplace'

export type DeploymentSummary = {
  states: SkillProgress[]
  aggregate: 'installed' | 'applying' | 'failed' | 'waiting' | 'removed' | 'none'
  failure?: string
}

const aggregate = (items: SkillProgress[]): DeploymentSummary['aggregate'] => {
  if (items.length === 0) return 'none'
  if (items.some((item) => item.state === 'failed')) return 'failed'
  if (items.every((item) => item.state === 'installed')) return 'installed'
  if (items.every((item) => item.state === 'removed')) return 'removed'
  if (items.some((item) => item.state === 'applying')) return 'applying'
  return 'waiting'
}

/**
 * Reads each selected Skill's per-project delivery receipts. A selection alone does not prove the
 * package reached any project, so the Skills page and the workflow Agent picker can show what has
 * actually been delivered, what is still in flight, and what failed.
 */
export function useSkillDeployments(
  tenant: string,
  space: string,
  selections: readonly SpacePlugin[],
) {
  const selected = selections.filter(
    (selection) => selection.kind === 'skill' && selection.desiredState === 'installed',
  )
  const query = useQuery({
    queryKey: ['skill-deployments', tenant, space, selected.map(pluginKey).join('|')],
    enabled: tenant !== '' && space !== '' && selected.length > 0,
    refetchInterval: 3000,
    queryFn: async () => {
      const rows = await Promise.all(
        selected.map(async (selection) => {
          const { items } = await getSkillProgress(
            tenant,
            space,
            selection.sourceNamespace,
            selection.identifier,
          )
          return [pluginKey(selection), items] as const
        }),
      )
      return new Map<string, SkillProgress[]>(rows)
    },
  })
  const byKey = query.data ?? new Map<string, SkillProgress[]>()
  return {
    byKey,
    /** The aggregate delivery state for one selection; `undefined` while the receipt is unknown. */
    summaryOf: (selection: SpacePlugin): DeploymentSummary | undefined => {
      const items = byKey.get(pluginKey(selection))
      if (items === undefined) return undefined
      const failed = items.find((item) => item.state === 'failed')
      return {
        states: items,
        aggregate: aggregate(items),
        failure: failed?.failure,
      }
    },
  }
}
