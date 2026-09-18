import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { installFakeHttp } from '@/test/http'
import { App } from './app'

function renderApp() {
  // Retries are disabled so the error path settles within one request.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>,
  )
}

describe('App', () => {
  it('reports the backend status from /healthz', async () => {
    const http = installFakeHttp({ status: 'ok' })

    renderApp()

    expect(await screen.findByText('backend status: ok')).toBeTruthy()
    expect(http.requests.map((request) => request.url)).toEqual(['/healthz'])
  })

  it('reports an unreachable backend', async () => {
    installFakeHttp({ code: 'unavailable' }, 503)

    renderApp()

    expect(await screen.findByText('backend unreachable')).toBeTruthy()
  })
})
