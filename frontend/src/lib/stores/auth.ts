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

// What a password check led to. 'session' means the user is signed in; the other two mean a
// second step is owed and carry the one-time challenge that step needs.
export type LoginOutcome =
	| { kind: 'session' }
	| { kind: 'mfa'; challengeToken: string }
	| { kind: 'enroll'; challengeToken: string };

export async function loginAndSetSession(email: string, password: string): Promise<LoginOutcome> {
	const result = await authApi.login({ email, password });
	if (result.accessToken) {
		setSession(result.accessToken, email);
		return { kind: 'session' };
	}
	if (result.mfaEnrollmentRequired && result.challengeToken) {
		return { kind: 'enroll', challengeToken: result.challengeToken };
	}
	if (result.mfaRequired && result.challengeToken) {
		return { kind: 'mfa', challengeToken: result.challengeToken };
	}
	// A 200 that is none of the above is a server contract break; do not guess.
	throw new Error('Unexpected sign-in response.');
}

export async function completeMfaLogin(
	email: string,
	challengeToken: string,
	code: string
): Promise<void> {
	const result = await authApi.verifyMfa(challengeToken, code);
	setSession(result.accessToken, email);
}

export const beginEnrollment = authApi.beginMfaEnrollment;

// Confirming enrolment signs the user in. The recovery codes come back with the session and are
// handed to the caller to display once; this store deliberately does not keep them.
export async function completeEnrollment(
	email: string,
	challengeToken: string,
	code: string
): Promise<string[]> {
	const result = await authApi.confirmMfaEnrollment(challengeToken, code);
	setSession(result.accessToken, email);
	return result.recoveryCodes;
}

export async function logoutAndClearSession(): Promise<void> {
	try {
		await authApi.logout();
	} finally {
		clearSession();
	}
}
