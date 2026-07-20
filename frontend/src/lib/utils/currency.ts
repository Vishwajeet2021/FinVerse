// Every amount on the wire is minor units (paise) per docs/08-database-design.md §3 —
// this is the one place the UI converts to/from the rupee amounts a customer types and reads.

export function rupeesToMinor(rupees: string): number | null {
	const n = Number(rupees);
	if (!Number.isFinite(n) || n <= 0) return null;
	return Math.round(n * 100);
}

export function formatMinorAsRupees(minor: number): string {
	return (minor / 100).toLocaleString('en-IN', {
		minimumFractionDigits: 2,
		maximumFractionDigits: 2
	});
}
