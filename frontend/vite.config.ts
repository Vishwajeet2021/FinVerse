import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-auto';
import { sveltekit } from '@sveltejs/kit/vite';
import type { Config } from '@sveltejs/kit';
import { defineConfig, loadEnv } from 'vite';

type ConnectSrc = NonNullable<
	NonNullable<NonNullable<Config['kit']>['csp']>['directives']
>['connect-src'];

// Content Security Policy (docs/22 F-1). The page may only talk to its own origin and to the
// API origins it is configured with; it may not be framed; it may not load plugins or foreign
// scripts. SvelteKit adds nonces for the inline scripts and styles it generates itself.
function connectSources(env: Record<string, string>, dev: boolean): string[] {
	const sources = ['self'];
	for (const [key, value] of Object.entries(env)) {
		if (key.startsWith('VITE_') && key.endsWith('_SERVICE_URL') && value) {
			sources.push(new URL(value).origin);
		}
	}
	// The dev server's hot-reload socket. Never present in a production build.
	if (dev) sources.push('ws://localhost:*', 'ws://127.0.0.1:*');
	return sources;
}

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, process.cwd(), 'VITE_');
	const dev = mode === 'development';

	return {
		plugins: [
			tailwindcss(),
			sveltekit({
				compilerOptions: {
					// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
					runes: ({ filename }) =>
						filename.split(/[/\\]/).includes('node_modules') ? undefined : true
				},

				// adapter-auto only supports some environments, see https://svelte.dev/docs/kit/adapter-auto for a list.
				// If your environment is not supported, or you settled on a specific environment, switch out the adapter.
				// See https://svelte.dev/docs/kit/adapters for more information about adapters.
				adapter: adapter(),

				csp: {
					mode: 'auto',
					directives: {
						'default-src': ['self'],
						'script-src': ['self'],
						'style-src': ['self', 'unsafe-inline'],
						'img-src': ['self', 'data:'],
						'font-src': ['self'],
						'connect-src': connectSources(env, dev) as ConnectSrc,
						'object-src': ['none'],
						'base-uri': ['self'],
						'form-action': ['self'],
						'frame-ancestors': ['none']
					}
				}
			})
		]
	};
});
