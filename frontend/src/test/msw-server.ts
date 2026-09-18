import { setupServer } from 'msw/node'
import { handlers } from '@/mocks/handlers/index'

export const server = setupServer(...handlers)
