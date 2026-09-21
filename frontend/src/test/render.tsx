import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router-dom'
import { SidebarProvider } from '@/components/ui/sidebar'
import { CurrentSpaceProvider } from '@/features/spaces/current-space'

/**
 * Fresh QueryClient per render so cached data never leaks between tests. The
 * current-space provider is always present: without cloud credentials its
 * queries stay disabled and pages fall back to the mock store, so existing
 * mock-backed tests keep working unchanged.
 */
export function renderWithProviders(
  ui: ReactElement,
  { route = '/', slug = '' }: { route?: string; slug?: string } = {},
) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <SidebarProvider>
          <CurrentSpaceProvider slug={slug}>{ui}</CurrentSpaceProvider>
        </SidebarProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

/**
 * Mounts `element` behind a real route match (`path`), so hooks like
 * `useParams` resolve from `initialPath` the way they do in the app router.
 */
export function renderAtRoute(path: string, element: ReactElement, initialPath: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter([{ path, element }], { initialEntries: [initialPath] })
  return render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <CurrentSpaceProvider slug={initialPath.split('/')[1] ?? ''}>
          <RouterProvider router={router} />
        </CurrentSpaceProvider>
      </SidebarProvider>
    </QueryClientProvider>,
  )
}
