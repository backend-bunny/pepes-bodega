import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// In dev, proxy API/auth calls to the API (port-forwarded by Tilt to :8080).
const api = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.BODEGA_API ?? 'http://localhost:8080';

export default defineConfig({
	plugins: [
		sveltekit({
			// Pure SPA: the Go API serves build/ and falls back to index.html.
			adapter: adapter({ fallback: 'index.html' })
		})
	],
	server: {
		proxy: {
			'/api': api,
			'/auth': api
		}
	}
});
