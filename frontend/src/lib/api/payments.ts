// Typed client for Payment Service (docs/07-microservice-specifications.md §7).
import { PAYMENT_SERVICE_URL } from '$lib/config';
import { apiRequest } from './http';

export type Rail = 'INTERNAL' | 'UPI' | 'IMPS' | 'NEFT' | 'RTGS';

export interface Payment {
	id: string;
	fromAccountId: string;
	toReference: string;
	rail: Rail | string;
	amountMinor: number;
	status: 'INITIATED' | 'PROCESSING' | 'COMPLETED' | 'FAILED' | 'REVERSED';
	transactionId?: string;
	failureReason?: string;
	// Set while an asynchronous rail (UPI/IMPS/NEFT/RTGS) is still clearing.
	settleAt?: string;
}

export interface ScheduledPayment {
	id: string;
	fromAccountId: string;
	toReference: string;
	rail: Rail | string;
	amountMinor: number;
	runAt: string;
	status: 'PENDING' | 'EXECUTED' | 'CANCELLED' | string;
	paymentId?: string;
}

export type Frequency = 'DAILY' | 'WEEKLY' | 'MONTHLY';

export interface StandingInstruction {
	id: string;
	fromAccountId: string;
	toReference: string;
	rail: Rail | string;
	amountMinor: number;
	frequency: Frequency | string;
	nextRunAt: string;
	status: 'ACTIVE' | 'PAUSED' | 'CANCELLED' | string;
	lastPaymentId?: string;
}

// A fresh key per call — docs/09-api-standards.md §2.6 requires the caller
// supply one; crypto.randomUUID() is available in all evergreen browsers.
function newIdempotencyKey(): string {
	return crypto.randomUUID();
}

// `rail` is optional; the server treats a missing rail as INTERNAL, so existing
// callers that pass three arguments keep working unchanged.
export function initiatePayment(
	fromAccountId: string,
	toAccountNumber: string,
	amountMinor: number,
	rail?: Rail
): Promise<Payment> {
	return apiRequest<Payment>(PAYMENT_SERVICE_URL, '/api/v1/payments', {
		method: 'POST',
		auth: true,
		body: { fromAccountId, toAccountNumber, amountMinor, ...(rail ? { rail } : {}) },
		headers: { 'Idempotency-Key': newIdempotencyKey() }
	});
}

export function getPayment(paymentId: string): Promise<Payment> {
	return apiRequest<Payment>(PAYMENT_SERVICE_URL, `/api/v1/payments/${paymentId}`, {
		method: 'GET',
		auth: true
	});
}

export interface ScheduleInput {
	fromAccountId: string;
	toAccountNumber: string;
	amountMinor: number;
	rail?: Rail;
}

export function createScheduledPayment(
	input: ScheduleInput & { runAt: string }
): Promise<ScheduledPayment> {
	return apiRequest<ScheduledPayment>(PAYMENT_SERVICE_URL, '/api/v1/payments/scheduled', {
		method: 'POST',
		auth: true,
		body: input
	});
}

export function listScheduledPayments(): Promise<ScheduledPayment[]> {
	return apiRequest<ScheduledPayment[]>(PAYMENT_SERVICE_URL, '/api/v1/payments/scheduled', {
		method: 'GET',
		auth: true
	});
}

export function cancelScheduledPayment(id: string): Promise<void> {
	return apiRequest<void>(PAYMENT_SERVICE_URL, `/api/v1/payments/scheduled/${id}`, {
		method: 'DELETE',
		auth: true
	});
}

export function createStandingInstruction(
	input: ScheduleInput & { frequency: Frequency; firstRunAt: string }
): Promise<StandingInstruction> {
	return apiRequest<StandingInstruction>(
		PAYMENT_SERVICE_URL,
		'/api/v1/payments/standing-instructions',
		{ method: 'POST', auth: true, body: input }
	);
}

export function listStandingInstructions(): Promise<StandingInstruction[]> {
	return apiRequest<StandingInstruction[]>(
		PAYMENT_SERVICE_URL,
		'/api/v1/payments/standing-instructions',
		{ method: 'GET', auth: true }
	);
}

// pause/resume are POST actions and cancel is DELETE on the same resource.
export function pauseStandingInstruction(id: string): Promise<void> {
	return apiRequest<void>(
		PAYMENT_SERVICE_URL,
		`/api/v1/payments/standing-instructions/${id}/pause`,
		{ method: 'POST', auth: true }
	);
}

export function resumeStandingInstruction(id: string): Promise<void> {
	return apiRequest<void>(
		PAYMENT_SERVICE_URL,
		`/api/v1/payments/standing-instructions/${id}/resume`,
		{ method: 'POST', auth: true }
	);
}

export function cancelStandingInstruction(id: string): Promise<void> {
	return apiRequest<void>(PAYMENT_SERVICE_URL, `/api/v1/payments/standing-instructions/${id}`, {
		method: 'DELETE',
		auth: true
	});
}
