import type { Rail } from '$lib/api/payments';

export const RAIL_OPTIONS: { value: Rail; label: string; hint: string }[] = [
	{ value: 'INTERNAL', label: 'Instant (FinVerse)', hint: 'Between FinVerse accounts, immediate.' },
	{ value: 'UPI', label: 'UPI', hint: 'Up to ₹1,00,000. Clears in a few seconds.' },
	{ value: 'IMPS', label: 'IMPS', hint: 'Up to ₹5,00,000. Clears in a few seconds.' },
	{
		value: 'NEFT',
		label: 'NEFT',
		hint: 'Settles in the next batch, usually within a minute here.'
	},
	{ value: 'RTGS', label: 'RTGS', hint: 'Minimum ₹2,00,000. Clears in a few seconds.' }
];

// Failure reasons from Payment Service look like "CODE: customer-safe text". Show only the
// text; anything that doesn't follow the shape gets a generic message rather than leaking
// raw internal errors to a customer (docs/09-api-standards.md §2.4).
export function describePaymentFailure(reason: string | undefined): string {
	const match = reason?.match(/^[A-Z_]+:\s*(.+)$/);
	if (match) return match[1].charAt(0).toUpperCase() + match[1].slice(1);
	return 'The transfer could not be completed. No money was moved.';
}

// datetime-local gives "YYYY-MM-DDTHH:mm" in the user's local time; the API wants RFC 3339.
export function localInputToIso(value: string): string | null {
	if (!value) return null;
	const d = new Date(value);
	return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

export function formatWhen(iso: string): string {
	return new Date(iso).toLocaleString('en-IN', { dateStyle: 'medium', timeStyle: 'short' });
}
