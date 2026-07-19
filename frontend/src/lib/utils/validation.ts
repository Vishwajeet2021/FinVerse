// Client-side pre-checks only — the server (Auth Service) remains the source of truth
// (e.g. WEAK_PASSWORD, breached-password checks per docs/10-authentication-authorization.md §8).
// These exist purely so a user gets instant feedback before a round trip.

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function isValidEmail(email: string): boolean {
	return EMAIL_PATTERN.test(email);
}

// Mirrors the 12-character minimum from docs/10-authentication-authorization.md §8.
const MIN_PASSWORD_LENGTH = 12;

export function isPasswordLongEnough(password: string): boolean {
	return password.length >= MIN_PASSWORD_LENGTH;
}

export function passwordsMatch(password: string, confirmPassword: string): boolean {
	return password.length > 0 && password === confirmPassword;
}
