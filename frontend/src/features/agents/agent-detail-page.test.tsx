import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderAtRoute } from '@/test/render'
import { db } from '@/mocks/data/store'
import { AgentDetailPage } from './agent-detail-page'

describe('AgentDetailPage', () => {
  it("renders the agent's name, role, and assigned issues", async () => {
    const agent = db.agents[0]
    renderAtRoute(
      '/w/:workspaceSlug/agents/:agentId',
      <AgentDetailPage slug={db.workspace.slug} />,
      `/w/${db.workspace.slug}/agents/${agent.id}`,
    )

    expect(await screen.findAllByText(agent.name)).not.toHaveLength(0)
    expect(screen.getByText(agent.role)).toBeInTheDocument()

    const assigned = db.issues.filter((i) => i.assigneeId === agent.id)
    expect(
      await screen.findByText(new RegExp(`已分配任务（${assigned.length}）`)),
    ).toBeInTheDocument()
  })
})
