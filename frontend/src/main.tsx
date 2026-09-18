import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import { App } from './app'

const rootElement = document.getElementById('root')
if (rootElement === null) {
  // index.html owns the mount point; a missing element is a build error, not a runtime state.
  throw new Error('index.html must contain <div id="root">')
}

const queryClient = new QueryClient()

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
