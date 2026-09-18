import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderAtRoute } from '@/test/render'
import { db } from '@/mocks/data/store'
import { ChatPage } from './chat-page'

function renderChat(sessionId?: string) {
  const path = sessionId ? `/${db.workspace.slug}/chat/${sessionId}` : `/${db.workspace.slug}/chat`
  return renderAtRoute(
    '/:workspaceSlug/chat/:sessionId?',
    <ChatPage slug={db.workspace.slug} />,
    path,
  )
}

describe('ChatPage', () => {
  it('lists every seeded session and shows the most recently updated one selected by default', async () => {
    renderChat()
    for (const session of db.chatSessions) {
      expect(await screen.findAllByText(session.title)).not.toHaveLength(0)
    }
    // The API sorts sessions by updatedAt desc, same as the component's default pick.
    const defaultSession = [...db.chatSessions].sort((a, b) =>
      b.updatedAt.localeCompare(a.updatedAt),
    )[0]
    if (!defaultSession) throw new Error('chat seed data must contain a session')
    const firstMessage = db.chatMessages
      .filter((m) => m.sessionId === defaultSession.id)
      .sort((a, b) => a.createdAt.localeCompare(b.createdAt))[0]!
    // The filler phrase bank is small and can repeat within a session, so
    // assert presence rather than a single unique match.
    expect(await screen.findAllByText(firstMessage.content)).not.toHaveLength(0)
  })

  it('sends a message and receives a canned agent reply', async () => {
    const session = db.chatSessions[1]
    if (!session) throw new Error('chat seed data must contain a second session')
    const user = userEvent.setup()
    renderChat(session.id)
    await screen.findAllByText(session.title)

    const beforeCount = db.chatMessages.filter((m) => m.sessionId === session.id).length

    await user.type(screen.getByPlaceholderText(/给智能体发消息/), 'Ping!')
    await user.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => {
      const afterCount = db.chatMessages.filter((m) => m.sessionId === session.id).length
      expect(afterCount).toBe(beforeCount + 2)
    })
    expect(await screen.findByText('Ping!')).toBeInTheDocument()
  })
})
