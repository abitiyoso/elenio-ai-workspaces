import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// AGENT_STUDIO_WEB_BASE_PATH lets the SPA live under a reverse-proxy prefix
// such as the Control Plane's /apps/agent-studio/. It must be an absolute path
// with a trailing slash made of plain URL segments; the default stays "/".
function webBasePath(raw: string | undefined): string {
  const value = (raw ?? '').trim();
  if (value === '') return '/';
  const segments = value.split('/').slice(1, -1);
  const valid = /^\/(?:[A-Za-z0-9._~-]+\/)*$/.test(value) && segments.every((segment) => segment !== '.' && segment !== '..');
  if (!valid) {
    throw new Error(`AGENT_STUDIO_WEB_BASE_PATH must look like /apps/agent-studio/ (got ${JSON.stringify(value)})`);
  }
  return value;
}

const base = webBasePath(process.env.AGENT_STUDIO_WEB_BASE_PATH);
const apiPrefix = `${base}api`;

export default defineConfig({
  base,
  plugins: [react()],
  server: {
    proxy: {
      // The Go API itself is always unprefixed (the Control Plane strips the
      // prefix), so the dev proxy strips it too.
      [apiPrefix]: {
        target: 'http://127.0.0.1:8190',
        rewrite: (path) => (base === '/' ? path : path.slice(base.length - 1)),
      },
    },
  },
});
