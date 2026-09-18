import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import './index.css'
import { router } from './routes'

const rootElement = document.getElementById('root')
if (rootElement === null) {
  // index.html owns the mount point; a missing element is a build error, not a runtime state.
  throw new Error('index.html must contain <div id="root">')
}
const mountNode = rootElement

const queryClient = new QueryClient()

async function bootstrap() {
  const { worker } = await import('./mocks/browser')
  await worker.start({ onUnhandledRequest: 'bypass' })

  createRoot(mountNode).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  )
}

void bootstrap()
