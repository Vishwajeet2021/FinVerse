// Typed client for every Auth Service endpoint (services/auth-service/internal/transport/http).
// Ground truth: dto.go, handlers.go, routes.go — read directly rather than assumed.
import { AUTH_SERVICE_URL } from '$lib/config';
import { apiRequest } from './http';

export interface RegisterInput {
	email: string;
	phone?: string;
	password: string;
}

export interface RegisteredUser {
	id: string;
	email: string;
	phone?: string;
	role: string;
	emailVerified: boolean;
	status: string;
}

export interface LoginInput {
	email: string;
	password: string;
}

// The refresh token is deliberately absent here — it never leaves the HttpOnly cookie the
// browser manages automatically (docs/10-authentication-authorization.md §2).
export interface TokenResult {
	accessToken: string;
	tokenType: string;
}

export function register(input: RegisterInput): Promise<RegisteredUser> {
	return apiRequest<RegisteredUser>(AUTH_SERVICE_URL, '/api/v1/auth/register', {
		method: 'POST',
		body: input
	});
}

// A password check ends in one of three ways (docs/24-admin-mfa.md): a session, a request for a
// code from the user's authenticator, or - for roles that must have two-step verification and
// have not set it up yet - a request to set it up. The challenge token proves only that the
// password step passed; it opens no API and is held in memory for the length of the sign-in.
export interface LoginResult {
	accessToken?: string;
	tokenType?: string;
	mfaRequired?: boolean;
	mfaEnrollmentRequired?: boolean;
	challengeToken?: string;
}

export function login(input: LoginInput): Promise<LoginResult> {
	return apiRequest<LoginResult>(AUTH_SERVICE_URL, '/api/v1/auth/login', {
		method: 'POST',
		body: input
	});
}

export function verifyMfa(challengeToken: string, code: string): Promise<TokenResult> {
	return apiRequest<TokenResult>(AUTH_SERVICE_URL, '/api/v1/auth/mfa/verify', {
		method: 'POST',
		body: { challengeToken, code }
	});
}

export interface MfaEnrollment {
	secret: string;
	otpauthUri: string;
}

export function beginMfaEnrollment(challengeToken: string): Promise<MfaEnrollment> {
	return apiRequest<MfaEnrollment>(AUTH_SERVICE_URL, '/api/v1/auth/mfa/enroll/begin', {
		method: 'POST',
		body: { challengeToken }
	});
}

// Finishes enrolment and the sign-in together. The recovery codes in the response are shown to
// the user exactly once; the server keeps only their hashes.
export interface MfaEnrollmentSession extends TokenResult {
	recoveryCodes: string[];
}

export function confirmMfaEnrollment(
	challengeToken: string,
	code: string
): Promise<MfaEnrollmentSession> {
	return apiRequest<MfaEnrollmentSession>(AUTH_SERVICE_URL, '/api/v1/auth/mfa/enroll/confirm', {
		method: 'POST',
		body: { challengeToken, code }
	});
}

// Relies solely on the HttpOnly refresh cookie sent via credentials: 'include' — no body.
export function refresh(): Promise<TokenResult> {
	return apiRequest<TokenResult>(AUTH_SERVICE_URL, '/api/v1/auth/refresh', { method: 'POST' });
}

export function logout(): Promise<void> {
	return apiRequest<void>(AUTH_SERVICE_URL, '/api/v1/auth/logout', { method: 'POST' });
}

export function logoutAll(): Promise<void> {
	return apiRequest<void>(AUTH_SERVICE_URL, '/api/v1/auth/logout-all', {
		method: 'POST',
		auth: true
	});
}

export function verifyEmail(token: string): Promise<{ verified: true }> {
	return apiRequest(AUTH_SERVICE_URL, '/api/v1/auth/verify-email', {
		method: 'POST',
		body: { token }
	});
}

export function requestPasswordReset(email: string): Promise<{ message: string }> {
	return apiRequest(AUTH_SERVICE_URL, '/api/v1/auth/password-reset/request', {
		method: 'POST',
		body: { email }
	});
}

export function confirmPasswordReset(token: string, newPassword: string): Promise<{ reset: true }> {
	return apiRequest(AUTH_SERVICE_URL, '/api/v1/auth/password-reset/confirm', {
		method: 'POST',
		body: { token, newPassword }
	});
}
