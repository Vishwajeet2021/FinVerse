// Typed client for Reporting Service (docs/23).
import { REPORTING_SERVICE_URL } from '$lib/config';
import { apiDownload, apiRequest, type DownloadedFile } from './http';

export type ReportStatus = 'QUEUED' | 'RUNNING' | 'READY' | 'FAILED' | 'EXPIRED';

export interface Report {
	id: string;
	kind: 'STATEMENT' | 'FRAUD_SUMMARY';
	accountId?: string;
	from?: string;
	to?: string;
	status: ReportStatus;
	rowCount: number;
	sizeBytes: number;
	failureCode?: string;
	createdAt: string;
	completedAt?: string;
	expiresAt?: string;
}

export function requestStatement(accountId: string, from: string, to: string): Promise<Report> {
	return apiRequest<Report>(REPORTING_SERVICE_URL, '/api/v1/reports/statements', {
		method: 'POST',
		auth: true,
		body: { accountId, from, to }
	});
}

export function requestFraudSummary(): Promise<Report> {
	return apiRequest<Report>(REPORTING_SERVICE_URL, '/api/v1/reports/fraud-summary', {
		method: 'POST',
		auth: true,
		body: {}
	});
}

export function listReports(): Promise<Report[]> {
	return apiRequest<Report[]>(REPORTING_SERVICE_URL, '/api/v1/reports?limit=20', { auth: true });
}

export function downloadReport(id: string): Promise<DownloadedFile> {
	return apiDownload(REPORTING_SERVICE_URL, `/api/v1/reports/${encodeURIComponent(id)}/download`);
}

// What a failed report means to a customer. The server only ever sends a short code.
export function describeReportFailure(code?: string): string {
	switch (code) {
		case 'TOO_LARGE':
			return 'This account has too much activity for one report. Try a shorter date range.';
		case 'MIXED_CURRENCY':
			return 'This account holds more than one currency, which statements do not support yet.';
		default:
			return "We couldn't generate this report. Please try again.";
	}
}
