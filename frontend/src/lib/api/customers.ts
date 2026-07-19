// Typed client for Customer Service (docs/07-microservice-specifications.md §3).
// Customer Service is still being built in parallel — this module and its callers
// (the /dashboard route) must not assume it is reachable; http.ts surfaces an
// unreachable service as an ApiError with code NETWORK_ERROR, which callers branch on
// exactly like any other ApiError.
import { CUSTOMER_SERVICE_URL } from '$lib/config';
import { apiRequest } from './http';

// Roughly per docs/07-microservice-specifications.md §3 — the service's contract may still
// grow fields; this client only models what the dashboard currently displays.
export interface CustomerProfile {
	id: string;
	fullName: string;
	dob: string;
	kycTier: number;
	kycStatus: string;
}

export function getMyProfile(): Promise<CustomerProfile> {
	return apiRequest<CustomerProfile>(CUSTOMER_SERVICE_URL, '/api/v1/customers/me', {
		method: 'GET',
		auth: true
	});
}

export function updateMyProfile(patch: Partial<CustomerProfile>): Promise<CustomerProfile> {
	return apiRequest<CustomerProfile>(CUSTOMER_SERVICE_URL, '/api/v1/customers/me', {
		method: 'PATCH',
		auth: true,
		body: patch
	});
}
