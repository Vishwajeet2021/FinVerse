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

// Mirrors the server's customer rules (services/customer-service/internal/domain/validation.go).
const PAN_PATTERN = /^[A-Z]{5}[0-9]{4}[A-Z]$/;

export function isValidPan(pan: string): boolean {
	return PAN_PATTERN.test(pan);
}

export const MAX_PASSWORD_LENGTH = 128;
export const MAX_ACCOUNT_NUMBER_LENGTH = 32;

// The caller must be an adult (18 to 120 years old on `now`).
export function isAdultDob(dob: string, now: Date = new Date()): boolean {
	const d = new Date(dob);
	if (Number.isNaN(d.getTime())) return false;
	const oldest = new Date(now);
	oldest.setFullYear(now.getFullYear() - 120);
	const youngest = new Date(now);
	youngest.setFullYear(now.getFullYear() - 18);
	return d >= oldest && d <= youngest;
}
