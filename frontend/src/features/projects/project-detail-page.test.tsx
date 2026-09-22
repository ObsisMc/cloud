import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, describe, expect, it } from 'vitest'
import type { SpaceListItem } from '@/api/generated.schemas'
import { ProjectDetailPage } from './project-detail-page'
import { db } from '@/mocks/data/store'
import { setCloudSession, TEST_SPACE_ID, TEST_TENANT_ID } from '@/test/cloud-session'
import { server } from '@/test/msw-server'
import { renderAtRoute } from '@/test/render'
import { useAuthStore } from '@/state/auth-store'

const PROJECT_ID = '55555555-5555-5555-5555-555555555555'
const CURRENT_USER_ID = 'u1' // setCloudSession signs the tab in as u1

function spaceItem(role: string): SpaceListItem {
  return {
    id: TEST_SPACE_ID,
    tenantId: TEST_TENANT_ID,
    name: 'Team Space',
    slug: 'team',
    description: '',
    role,
    createdBy: CURRENT_USER_ID,
    version: 1,
    createdAt: '2026-09-21T10:00:00+08:00',
    updatedAt: '2026-09-21T10:00:00+08:00',
    archivedAt: null,
  }
}

function cloudProject(ownerUserId: string) {
  return {
    id: PROJECT_ID,
    tenantId: TEST_TENANT_ID,
    ownerUserId,
    name: 'ProjectAlpha',
    repositoryUrl: 'https://example.invalid/repo.git',
    defaultBranch: 'main',
    lifecycle: 'active',
    spaceId: TEST_SPACE_ID,
    version: 1,
    createdAt: '2026-09-21T10:00:00+08:00',
    credentialRefId: null,
    deletedAt: null,
  }
}

/** Renders the project detail page in cloud mode for a member with `role`. */
async function renderCloudProject(role: string, ownerUserId: string) {
  setCloudSession()
  server.use(
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces`, () =>
      HttpResponse.json({ items: [spaceItem(role)], nextCursor: '' }),
    ),
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/projects`, () =>
      HttpResponse.json({ items: [cloudProject(ownerUserId)], nextCursor: '' }),
    ),
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/projects/${PROJECT_ID}`, () =>
      HttpResponse.json(cloudProject(ownerUserId)),
    ),
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/issues`, () =>
      HttpResponse.json({ items: [], nextCursor: '' }),
    ),
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/members`, () =>
      HttpResponse.json({ items: [], nextCursor: '' }),
    ),
  )
  renderAtRoute(
    '/:workspaceSlug/projects/:projectId',
    <ProjectDetailPage slug="team" />,
    `/team/projects/${PROJECT_ID}`,
  )
  // The title renders in both the page header h1 and the body h1, so a single
  // finder throws on the multiple match; wait for at least one instead.
  expect((await screen.findAllByText('ProjectAlpha')).length).toBeGreaterThan(0)
}

describe('ProjectDetailPage', () => {
  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('renders the project header and its issues', async () => {
    const project = db.projects[0]
    if (!project) throw new Error('project seed data must not be empty')
    renderAtRoute(
      '/:workspaceSlug/projects/:projectId',
      <ProjectDetailPage slug={db.workspace.slug} />,
      `/${db.workspace.slug}/projects/${project.id}`,
    )

    expect(await screen.findAllByText(project.title)).not.toHaveLength(0)

    const projectIssue = db.issues.find((i) => i.projectId === project.id)
    if (!projectIssue) throw new Error('project seed data must contain an issue')
    expect(await screen.findByText(projectIssue.title)).toBeInTheDocument()
  })

  it('shows delete for the member who created the project (creator rule)', async () => {
    await renderCloudProject('member', CURRENT_USER_ID)
    expect(screen.getByRole('button', { name: '删除项目' })).toBeInTheDocument()
  })

  it('hides delete for a member who is not the creator', async () => {
    await renderCloudProject('member', 'u2')
    expect(screen.queryByRole('button', { name: '删除项目' })).not.toBeInTheDocument()
  })

  it('shows delete for a workspace admin who is not the creator', async () => {
    await renderCloudProject('admin', 'u2')
    expect(screen.getByRole('button', { name: '删除项目' })).toBeInTheDocument()
  })

  it('shows delete for a workspace owner who is not the creator', async () => {
    await renderCloudProject('owner', 'u2')
    expect(screen.getByRole('button', { name: '删除项目' })).toBeInTheDocument()
  })
})
