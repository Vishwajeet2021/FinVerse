import { ApiError } from '$lib/api/http';

// Every page that surfaces an API failure goes through this so we never show a raw
// fetch/JS error to a customer (docs/09-api-standards.md §2.4, NFR-UX-3).
export function describeError(
	err: unknown,
	fallback = 'Something went wrong. Please try again.'
): string {
	if (err instanceof ApiError) return err.message;
	return fallback;
}
