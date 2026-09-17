import { defineConfig } from 'orval'

export default defineConfig({
  oraCloud: {
    input: {
      target: '../api/openapi.json',
    },
    output: {
      mode: 'tags-split',
      target: 'src/api/generated.ts',
      client: 'react-query',
      httpClient: 'axios',
      baseUrl: false,
      clean: true,
      override: {
        mutator: {
          path: 'src/lib/api-client.ts',
          name: 'customInstance',
        },
      },
    },
  },
})
