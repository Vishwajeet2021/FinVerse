// Typed client for Admin Service (docs/23). Every call here is refused by the server unless
// the signed-in user holds the matching admin scope; the UI hides nothing the server allows.
import { ADMIN_SERVICE_URL } from '$lib/config';
import { apiRequest } from './http';

export interface Section<T> {
	available: boolean;
	data?: T;
}

export interface FraudCase {
	id: string;
	accountId: string;
	paymentId?: string;
	amountMinor: number;
	verdict: string;
	rule: string;
	reason: string;
	riskScore: number;
	status: string;
	createdAt: string;
}

export interface Dashboard {
	generatedAt: string;
	fraud: Section<{ openCases: number; recent: FraudCase[] }>;
	payments: Section<{ initiated1h: number; completed1h: number; failed1h: number }>;
	ops: Section<{ deadLetterDepth: number; firingAlerts: string[]; servicesDown: number }>;
}

export interface AdminAction {
	id: string;
	actorId: string;
	type: 'ACCOUNT_FREEZE' | 'ACCOUNT_UNFREEZE' | 'FRAUD_CASE_RESOLVE' | 'KYC_REVIEW';
	targetType: string;
	targetId: string;
	reason: string;
	status: 'REQUESTED' | 'SUCCEEDED' | 'FAILED';
	failureReason?: string;
	createdAt: string;
	resolvedAt?: string;
}

const get = <T>(path: string) => apiRequest<T>(ADMIN_SERVICE_URL, path, { auth: true });
const post = <T>(path: string, body: unknown) =>
	apiRequest<T>(ADMIN_SERVICE_URL, path, { method: 'POST', auth: true, body });

export const getDashboard = () => get<Dashboard>('/api/v1/admin/dashboard');
export const listActions = (limit = 20) =>
	get<AdminAction[]>(`/api/v1/admin/actions?limit=${limit}`);

export const freezeAccount = (accountId: string, reason: string) =>
	post<AdminAction>(`/api/v1/admin/accounts/${encodeURIComponent(accountId)}/freeze`, { reason });
export const unfreezeAccount = (accountId: string, reason: string) =>
	post<AdminAction>(`/api/v1/admin/accounts/${encodeURIComponent(accountId)}/unfreeze`, { reason });
export const resolveFraudCase = (
	caseId: string,
	outcome: 'CONFIRMED' | 'DISMISSED',
	reason: string
) =>
	post<AdminAction>(`/api/v1/admin/fraud/cases/${encodeURIComponent(caseId)}/resolve`, {
		outcome,
		reason
	});
export const reviewKyc = (customerId: string, approve: boolean, tier: number, reason: string) =>
	post<AdminAction>(`/api/v1/admin/customers/${encodeURIComponent(customerId)}/kyc-review`, {
		approve,
		tier,
		reason
	});

export const MIN_REASON = 5;
export const MAX_REASON = 500;
