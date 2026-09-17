import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderAtRoute } from '@/test/render'
import { db } from '@/mocks/data/store'
import { AgentDetailPage } from './agent-detail-page'

describe('AgentDetailPage', () => {
  it("renders the agent's name, role, and assigned issues", async () => {
    const agent = db.agents[0]
    renderAtRoute(
      '/:workspaceSlug/agents/:agentId',
      <AgentDetailPage slug={db.workspace.slug} />,
      `/${db.workspace.slug}/agents/${agent.id}`,
    )

    expect(await screen.findAllByText(agent.name)).not.toHaveLength(0)
    expect(screen.getByText(agent.role)).toBeInTheDocument()

    const assigned = db.issues.filter((i) => i.assigneeId === agent.id)
    expect(await screen.findByText(new RegExp(`Assigned issues \\(${assigned.length}\\)`))).toBeInTheDocument()
  })
})
