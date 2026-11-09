import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// KMS_BASE_PATH lets the UI be served under a sub-path, e.g. /apps/kms/ behind the AI Workspace Control Plane
// BFF. It must start and end with "/" and contain only plain path segments. The default "/" keeps the
// standalone build unchanged (assets at /assets/...).
function basePath(): string {
  const value = (process.env.KMS_BASE_PATH ?? '').trim() || '/'
  if (value !== '/' && !/^\/(?:[A-Za-z0-9_~-][A-Za-z0-9._~-]*\/)+$/.test(value)) {
    throw new Error(`KMS_BASE_PATH must look like /apps/kms/ (leading and trailing slash, plain segments); got ${JSON.stringify(value)}`)
  }
  return value
}

const base = basePath()
const backend = 'http://localhost:8080'
const proxy: Record<string, string | { target: string; rewrite: (path: string) => string }> = { '/api': backend, '/healthz': backend, '/readyz': backend }
if (base !== '/') {
  // In `vite dev` with a sub-path, strip the prefix like the Control Plane BFF does.
  const prefix = base.slice(0, -1)
  for (const route of ['/api', '/healthz', '/readyz']) {
    proxy[prefix + route] = { target: backend, rewrite: path => path.slice(prefix.length) }
  }
}

export default defineConfig({
  base,
  plugins: [react()],
  server: { proxy },
})
