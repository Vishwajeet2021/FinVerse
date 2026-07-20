// Typed client for Payment Service (docs/07-microservice-specifications.md §7).
import { PAYMENT_SERVICE_URL } from '$lib/config';
import { apiRequest } from './http';

export interface Payment {
	id: string;
	fromAccountId: string;
	toReference: string;
	rail: string;
	amountMinor: number;
	status: 'INITIATED' | 'PROCESSING' | 'COMPLETED' | 'FAILED' | 'REVERSED';
	transactionId?: string;
	failureReason?: string;
}

// A fresh key per call — docs/09-api-standards.md §2.6 requires the caller
// supply one; crypto.randomUUID() is available in all evergreen browsers.
function newIdempotencyKey(): string {
	return crypto.randomUUID();
}

export function initiatePayment(
	fromAccountId: string,
	toAccountId: string,
	amountMinor: number
): Promise<Payment> {
	return apiRequest<Payment>(PAYMENT_SERVICE_URL, '/api/v1/payments', {
		method: 'POST',
		auth: true,
		body: { fromAccountId, toAccountId, amountMinor },
		headers: { 'Idempotency-Key': newIdempotencyKey() }
	});
}

export function getPayment(paymentId: string): Promise<Payment> {
	return apiRequest<Payment>(PAYMENT_SERVICE_URL, `/api/v1/payments/${paymentId}`, {
		method: 'GET',
		auth: true
	});
}
