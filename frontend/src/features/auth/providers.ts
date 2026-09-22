import { useQuery } from '@tanstack/react-query'
import { fetchLoginProviders } from '@/features/auth/api'

/** Query key of the provider list; it only changes with the gateway configuration. */
export const PROVIDERS_QUERY_KEY = ['login-providers'] as const

/**
 * The logins the gateway offers, cached for the tab's lifetime. The login
 * page renders one button per entry; the sidebar uses it to decide whether
 * a GitHub sign-out makes sense.
 */
export function useLoginProviders() {
  return useQuery({
    queryKey: PROVIDERS_QUERY_KEY,
    queryFn: ({ signal }) => fetchLoginProviders(signal),
    retry: false,
    staleTime: Infinity,
  })
}
