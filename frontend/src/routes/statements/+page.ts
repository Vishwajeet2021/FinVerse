import { redirect } from '@sveltejs/kit';
import { get } from 'svelte/store';
import { ensureAuthInitialized, isAuthenticated } from '$lib/stores/auth';

// The auth check depends on the in-memory access token, which exists only in the browser
// (docs/10-authentication-authorization.md §2) — there's no server-side session to render
// against, so this route is client-only rather than pretending to SSR protected content.
export const ssr = false;

export async function load() {
	await ensureAuthInitialized();
	if (!get(isAuthenticated)) {
		throw redirect(303, '/login');
	}
	return {};
}
