import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { installCloudSpaceHandlers, TEST_SPACE_ID, TEST_TENANT_ID } from '@/test/cloud-handlers'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'
import { ProjectsPage } from './projects-page'

function cloudProject(id: string, name: string) {
  return {
    id,
    tenantId: TEST_TENANT_ID,
    ownerUserId: 'u1',
    spaceId: TEST_SPACE_ID,
    name,
    repositoryUrl: `https://example.com/${name}.git`,
    defaultBranch: 'main',
    credentialRefId: null,
    lifecycle: 'active',
    version: 1,
    createdAt: '2026-09-20T10:00:00+08:00',
    deletedAt: null,
  }
}

describe('ProjectsPage', () => {
  it('renders every project of the resolved space as a card', async () => {
    installCloudSpaceHandlers('member')
    server.use(
      http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/projects`, () =>
        HttpResponse.json({
          items: [
            cloudProject('55555555-5555-5555-5555-555555555555', 'Alpha'),
            cloudProject('66666666-6666-6666-6666-666666666666', 'Beta'),
          ],
          nextCursor: '',
        }),
      ),
    )
    renderWithProviders(<ProjectsPage slug="cloud-dev" />, { slug: 'cloud-dev' })

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(await screen.findByText('Beta')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /新建项目/ })).toBeInTheDocument()
  })

  it('offers no project creation and shows a skeleton until the space resolved', () => {
    renderWithProviders(<ProjectsPage slug="cloud-dev" />, { slug: 'cloud-dev' })

    expect(screen.queryByRole('button', { name: /新建项目/ })).not.toBeInTheDocument()
    expect(screen.queryByText('Alpha')).not.toBeInTheDocument()
  })
})
