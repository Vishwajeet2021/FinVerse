// Client-only auth state (docs/12-coding-standards.md §2: Svelte stores for client-only
// state, TanStack Query for server data — the access token is neither "server data" to
// cache nor safe to persist, so a plain store is the right tool).
//
// The access token lives in memory only, per docs/10-authentication-authorization.md §2 —
// never localStorage, never decoded/trusted client-side for authorization decisions. We only
// track *whether* we have one, plus the email the user last authenticated with (which we
// already know locally from their own form input — this is bookkeeping, not a security claim).
import { derived, get, writable } from 'svelte/store';
import * as authApi from '$lib/api/auth';
import { registerAccessTokenGetter, registerRefreshHandler } from '$lib/api/http';

interface AuthState {
	accessToken: string | null;
	email: string | null;
}

const initialState: AuthState = { accessToken: null, email: null };

const state = writable<AuthState>(initialState);

export const isAuthenticated = derived(state, (s) => s.accessToken !== null);
export const currentUserEmail = derived(state, (s) => s.email);

function setSession(accessToken: string, email?: string | null): void {
	state.update((s) => ({ accessToken, email: email ?? s.email }));
}

function clearSession(): void {
	state.set(initialState);
}

registerAccessTokenGetter(() => get(state).accessToken);

// Backs the http.ts one-retry-via-refresh pattern: on a 401 from an `auth: true` request,
// http.ts calls this once before giving up. Living here (rather than in http.ts) keeps all
// session-mutation logic in one file.
registerRefreshHandler(async () => {
	try {
		const result = await authApi.refresh();
		setSession(result.accessToken);
		return result.accessToken;
	} catch {
		clearSession();
		return null;
	}
});

let initPromise: Promise<void> | null = null;

// Restores a session across a page reload without forcing re-login. The access token never
// survives a reload (it's memory-only), but the HttpOnly refresh cookie does — this is the one
// place we spend it to silently mint a fresh access token. Idempotent: safe to call from both
// the root layout (point of first load) and any route's `load` function that needs to know
// the resolved auth state before deciding whether to redirect.
export function ensureAuthInitialized(): Promise<void> {
	if (!initPromise) {
		initPromise = authApi
			.refresh()
			.then((result) => setSession(result.accessToken))
			.catch(() => clearSession());
	}
	return initPromise;
}

export async function loginAndSetSession(email: string, password: string): Promise<void> {
	const result = await authApi.login({ email, password });
	setSession(result.accessToken, email);
}

export async function logoutAndClearSession(): Promise<void> {
	try {
		await authApi.logout();
	} finally {
		clearSession();
	}
}
