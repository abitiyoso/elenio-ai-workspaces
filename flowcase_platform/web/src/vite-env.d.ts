/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Comma-separated parent workspace origins allowed to post the host token (PENDING host contract). */
  readonly VITE_HOST_ORIGINS?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
