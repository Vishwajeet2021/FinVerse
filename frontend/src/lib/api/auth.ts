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

export function login(input: LoginInput): Promise<TokenResult> {
	return apiRequest<TokenResult>(AUTH_SERVICE_URL, '/api/v1/auth/login', {
		method: 'POST',
		body: input
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
