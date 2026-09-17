import { http, HttpResponse } from 'msw'
import { currentUserId, db } from '../data/store'
import { MOCK_BASE } from './shared'

const MOCK_TOKEN = 'mock-session-token'

export const authHandlers = [
  http.post(`${MOCK_BASE}/auth/login`, async ({ request }) => {
    const body = (await request.json()) as { email?: string }
    const user = db.users.find((u) => u.id === currentUserId)!
    return HttpResponse.json({
      token: MOCK_TOKEN,
      user: { ...user, email: body?.email || user.email },
    })
  }),

  http.get(`${MOCK_BASE}/auth/session`, ({ request }) => {
    const auth = request.headers.get('authorization')
    if (auth !== `Bearer ${MOCK_TOKEN}`) {
      return HttpResponse.json({ message: 'unauthenticated' }, { status: 401 })
    }
    const user = db.users.find((u) => u.id === currentUserId)!
    return HttpResponse.json({ user })
  }),

  http.post(`${MOCK_BASE}/auth/logout`, () => new HttpResponse(null, { status: 204 })),
]
