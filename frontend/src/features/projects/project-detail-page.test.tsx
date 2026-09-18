import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderAtRoute } from '@/test/render'
import { db } from '@/mocks/data/store'
import { ProjectDetailPage } from './project-detail-page'

describe('ProjectDetailPage', () => {
  it('renders the project header and its issues', async () => {
    const project = db.projects[0]
    renderAtRoute(
      '/:workspaceSlug/projects/:projectId',
      <ProjectDetailPage slug={db.workspace.slug} />,
      `/${db.workspace.slug}/projects/${project.id}`,
    )

    expect(await screen.findAllByText(project.title)).not.toHaveLength(0)

    const projectIssue = db.issues.find((i) => i.projectId === project.id)
    if (projectIssue) {
      expect(await screen.findByText(projectIssue.title)).toBeInTheDocument()
    }
  })
})
