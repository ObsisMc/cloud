import { afterEach, expect, it } from 'vitest'
import { http } from '@/lib/api-client'
import { fakeBackend } from '@/test/backend'
import { workflowPluginCatalog } from './plugin-catalog'

const original = http.defaults.adapter
afterEach(() => { http.defaults.adapter = original })

it('offers a selected Skill before a project confirms it and keeps imported package identities', async () => {
  fakeBackend((config) => config.url === '/api/v1/tenants/t/spaces'
    ? { status: 200, data: { items: [{ id: 'space' }] } }
    : (config.url ?? '').includes('/plugins/results/')
      ? { status: 200, data: { items: [] } }
      : { status: 200, data: { items: [
        { sourceNamespace: 'official', identifier: 'skills', kind: 'skill', desiredState: 'installed', observedState: 'installed' },
        { sourceNamespace: 'custom', identifier: 'pending', kind: 'skill', desiredState: 'installed', observedState: 'pending' },
        { sourceNamespace: 'official', identifier: 'removed', kind: 'mcp', desiredState: 'removed', observedState: 'installed' },
        { sourceNamespace: 'official', identifier: 'node-missing', kind: 'mcp', desiredState: 'installed', observedState: 'pending' },
      ] } })
  const result = await workflowPluginCatalog('t')
  expect(result.skills.map((skill) => skill.name)).toEqual(['official/skills', 'custom/pending'])
  expect(result.skills.map((skill) => skill.deployment)).toEqual([
    { state: 'waiting' },
    { state: 'waiting' },
  ])
  expect(result.plugins).toEqual([])
})

it('annotates a selected Skill with its project delivery failure', async () => {
  fakeBackend((config) => config.url === '/api/v1/tenants/t/spaces'
    ? { status: 200, data: { items: [{ id: 'space' }] } }
    : (config.url ?? '').includes('/plugins/results/')
      ? { status: 200, data: { items: [
        { projectId: 'project', name: '项目', state: 'failed', installedVersion: '', failure: '技能包下载失败。' },
      ] } }
      : { status: 200, data: { items: [
        { sourceNamespace: 'official', identifier: 'skills', kind: 'skill', desiredState: 'installed', observedState: 'installed' },
      ] } })
  const result = await workflowPluginCatalog('t')
  expect(result.skills[0].deployment).toEqual({
    state: 'failed',
    failure: '技能包下载失败。',
  })
})

it('does not substitute demo plugins on a catalog failure', async () => {
  fakeBackend(() => ({ status: 503 }))
  await expect(workflowPluginCatalog('t')).rejects.toThrow()
})
