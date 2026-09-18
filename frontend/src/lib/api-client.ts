import { create, type AxiosError, type AxiosRequestConfig } from 'axios'

/**
 * Shared axios instance behind every generated hook in `src/api`.
 *
 * Cross-cutting HTTP policy (base URL, auth headers, interceptors) belongs here
 * so that generated code and hand-written code observe one configuration.
 */
export const AXIOS_INSTANCE = create({ baseURL: '' })

/**
 * Request shape the orval-generated client passes to {@link customInstance}.
 *
 * orval emits `signal: AbortSignal | undefined` rather than omitting the key,
 * so the type must accept an explicit `undefined` under
 * `exactOptionalPropertyTypes`.
 */
export type RequestConfig = Omit<AxiosRequestConfig, 'signal'> & {
  signal?: AbortSignal | undefined
}

/**
 * Promise returned to generated hooks; orval calls `cancel()` on it when a
 * query is torn down before the request settles.
 */
export type CancellablePromise<T> = Promise<T> & { cancel: () => void }

/**
 * orval mutator: executes one request and unwraps the response body.
 *
 * Cancellation has two sources that must both abort the request: the
 * `AbortSignal` react-query passes in `config`, and the legacy `cancel()`
 * method orval attaches to the returned promise.
 */
export const customInstance = <T>(
  config: RequestConfig,
  options?: AxiosRequestConfig,
): CancellablePromise<T> => {
  const controller = new AbortController()
  const signal =
    config.signal === undefined
      ? controller.signal
      : AbortSignal.any([config.signal, controller.signal])
  const promise = AXIOS_INSTANCE<T>({ ...config, ...options, signal }).then(({ data }) => data)
  return Object.assign(promise, { cancel: () => controller.abort() })
}

/** Error type generated hooks expose; the body is the server's `Fault` contract. */
export type ErrorType<Error> = AxiosError<Error>
