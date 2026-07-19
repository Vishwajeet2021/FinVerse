import { redirect } from '@sveltejs/kit';
import { get } from 'svelte/store';
import { ensureAuthInitialized, isAuthenticated } from '$lib/stores/auth';

// Deciding whether to redirect depends on the in-memory access token, which only ever
// exists in the browser (docs/10-authentication-authorization.md §2) — there is nothing
// meaningful to server-render here.
export const ssr = false;

export async function load() {
	await ensureAuthInitialized();
	if (get(isAuthenticated)) {
		throw redirect(303, '/dashboard');
	}
	return {};
}
