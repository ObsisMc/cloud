import {
  getApiV1TenantsTidSpaces,
  getApiV1TenantsTidSpacesSpaceIdPlugins,
} from '@/api/spaces/spaces'
import type { InstalledPlugin, Skill } from '../contracts'
import { getSkillProgress } from '@/features/settings/market-api/generated'

/** Aggregates one selected Skill's project receipts into the workflow picker's visible state. */
export type SkillDeployment =
  | { state: 'installed' }
  | { state: 'applying' }
  | { state: 'failed'; failure: string }
  | { state: 'waiting' }
  | { state: 'removed' }

/** A Skill the workflow Agent picker offers, annotated with its delivery state. */
export type CatalogSkill = Skill & { deployment?: SkillDeployment }

/** Definitions are tenant-wide; execution rechecks the chosen project's space and receipt. */
export async function workflowPluginCatalog(tenantId: string) {
  const spaces = await getApiV1TenantsTidSpaces(tenantId)
  const lists = await Promise.all(spaces.items.map((space) =>
    getApiV1TenantsTidSpacesSpaceIdPlugins(tenantId, space.id)))
  const selected = lists.flatMap((list) => list.items).filter((item) =>
    item.desiredState === 'installed')
  const unique = new Map(selected.map((item) => [`${item.sourceNamespace}/${item.identifier}`, item]))
  const skills: CatalogSkill[] = []
  const plugins: InstalledPlugin[] = []
  for (const [id, item] of unique) {
    // A Skill selection is the install, so the picker offers it without a project receipt; an MCP
    // server only exists once its Node reports it installed.
    if (item.kind === 'skill') {
      skills.push({
        id,
        namespace: item.sourceNamespace,
        name: id,
        description: item.identifier,
        source: { kind: 'plugin', pluginId: id },
        availability: 'available',
        deployment: await skillDeployment(tenantId, item.spaceId, item.sourceNamespace, item.identifier),
      })
    }
    if (item.kind === 'mcp' && item.observedState === 'installed') {
      plugins.push({ id, namespace: item.sourceNamespace, name: item.identifier,
        displayName: item.identifier, version: item.observedVersion ?? item.desiredVersion,
        description: '', homepage: null, license: null, logo: null, kind: 'mcp',
        installationValidity: { validity: 'valid' },
        configuration: { state: 'unavailable', errorCode: 'mcp_node_unsupported' }, runtime: 'stopped' })
    }
  }
  return { skills, plugins }
}

async function skillDeployment(
  tenant: string,
  space: string,
  namespace: string,
  identifier: string,
): Promise<SkillDeployment | undefined> {
  try {
    const { items } = await getSkillProgress(tenant, space, namespace, identifier)
    if (items.length === 0) return { state: 'waiting' }
    if (items.some((item) => item.state === 'failed'))
      return { state: 'failed', failure: items.find((item) => item.state === 'failed')?.failure ?? '' }
    if (items.every((item) => item.state === 'installed')) return { state: 'installed' }
    if (items.every((item) => item.state === 'removed')) return { state: 'removed' }
    if (items.some((item) => item.state === 'applying')) return { state: 'applying' }
    return { state: 'waiting' }
  } catch {
    return undefined
  }
}
