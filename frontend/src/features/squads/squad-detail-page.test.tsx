import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderAtRoute } from '@/test/render'
import { actorById, db } from '@/mocks/data/store'
import { SquadDetailPage } from './squad-detail-page'

describe('SquadDetailPage', () => {
  it('renders the squad name and its members', async () => {
    const squad = db.squads[0]
    if (!squad) throw new Error('squad seed data must not be empty')
    renderAtRoute(
      '/w/:workspaceSlug/squads/:squadId',
      <SquadDetailPage slug={db.workspace.slug} />,
      `/w/${db.workspace.slug}/squads/${squad.id}`,
    )

    expect(await screen.findAllByText(squad.name)).not.toHaveLength(0)
    const member = actorById(squad.memberIds[0])
    if (!member) throw new Error('squad seed data must contain a member')
    expect(await screen.findByText(member.name)).toBeInTheDocument()
  })
})
