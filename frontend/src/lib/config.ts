// Single place base URLs are read from env so every API client module agrees on them
// (docs/13-repository-structure.md §2 — one typed client per service under src/lib/api/).
export const AUTH_SERVICE_URL = import.meta.env.VITE_AUTH_SERVICE_URL ?? 'http://localhost:8081';

export const CUSTOMER_SERVICE_URL =
	import.meta.env.VITE_CUSTOMER_SERVICE_URL ?? 'http://localhost:8082';

export const ACCOUNT_SERVICE_URL =
	import.meta.env.VITE_ACCOUNT_SERVICE_URL ?? 'http://localhost:8083';

export const WALLET_SERVICE_URL =
	import.meta.env.VITE_WALLET_SERVICE_URL ?? 'http://localhost:8084';

export const PAYMENT_SERVICE_URL =
	import.meta.env.VITE_PAYMENT_SERVICE_URL ?? 'http://localhost:8085';
