import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, import.meta.dirname, 'JFE_');
  return {
    plugins: [react()],
    base: env.JFE_BASE_PATH || '/',
    server: {
      port: 3000,
      proxy: {
        '/api': env.JFE_API_PROXY || 'http://localhost:8090',
        '/openapi.json': env.JFE_API_PROXY || 'http://localhost:8090',
        '/docs': env.JFE_API_PROXY || 'http://localhost:8090',
      },
    },
  };
});
