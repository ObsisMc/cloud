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
  it('lists every seeded session and shows the first session selected by default', async () => {
    renderChat()
    for (const session of db.chatSessions) {
      expect(await screen.findAllByText(session.title)).not.toHaveLength(0)
    }
    const firstMessage = db.chatMessages.find((m) => m.sessionId === db.chatSessions[0].id)!
    expect(await screen.findByText(firstMessage.content)).toBeInTheDocument()
  })

  it('sends a message and receives a canned agent reply', async () => {
    const session = db.chatSessions[1]
    const user = userEvent.setup()
    renderChat(session.id)
    await screen.findAllByText(session.title)

    const beforeCount = db.chatMessages.filter((m) => m.sessionId === session.id).length

    await user.type(screen.getByPlaceholderText(/message the agent/i), 'Ping!')
    await user.click(screen.getByRole('button', { name: /send message/i }))

    await waitFor(() => {
      const afterCount = db.chatMessages.filter((m) => m.sessionId === session.id).length
      expect(afterCount).toBe(beforeCount + 2)
    })
    expect(await screen.findByText('Ping!')).toBeInTheDocument()
  })
})
