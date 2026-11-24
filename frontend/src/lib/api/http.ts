// Shared fetch wrapper — the only place a raw `fetch` call is allowed
// (docs/12-coding-standards.md §2: "no hand-rolled fetch calls scattered across components").
// Parses the {data, meta} / {error: {...}} envelope from docs/09-api-standards.md §2.3-2.4.

export interface ApiErrorDetail {
	code: string;
	message: string;
	details?: Record<string, unknown>;
	requestId?: string;
	traceId?: string;
}

interface ApiSuccessBody<T> {
	data: T;
	meta: { requestId: string; traceId?: string };
}

interface ApiErrorBody {
	error: ApiErrorDetail;
}

// Not part of any service's error registry (docs/09-api-standards.md §2.4 codes are
// server-issued) — this is the client's own code for "fetch itself failed" (DNS,
// connection refused, CORS), so callers can still branch on `.code` uniformly instead
// of special-casing network failures separately from API errors.
export const NETWORK_ERROR_CODE = 'NETWORK_ERROR';

export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly details?: Record<string, unknown>;
	readonly requestId?: string;

	constructor(
		status: number,
		code: string,
		message: string,
		details?: Record<string, unknown>,
		requestId?: string
	) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
		this.details = details;
		this.requestId = requestId;
	}
}

type AccessTokenGetter = () => string | null;
type RefreshHandler = () => Promise<string | null>;

let getAccessToken: AccessTokenGetter = () => null;
let refreshHandler: RefreshHandler | null = null;

// stores/auth.ts registers these at module init. Kept as plain module-level hooks
// (rather than http.ts importing the auth store directly) to avoid a circular import:
// stores/auth.ts already depends on api/auth.ts, which depends on this module.
export function registerAccessTokenGetter(getter: AccessTokenGetter): void {
	getAccessToken = getter;
}

export function registerRefreshHandler(handler: RefreshHandler): void {
	refreshHandler = handler;
}

export interface RequestOptions {
	method?: 'GET' | 'POST' | 'PATCH' | 'DELETE';
	body?: unknown;
	// Attaches `Authorization: Bearer <token>` and makes this request eligible for the
	// one-retry-via-refresh flow on a 401 (docs/10-authentication-authorization.md §2).
	// Left false for endpoints that are unauthenticated by contract (login/register/refresh/etc)
	// so their own 401s (e.g. INVALID_CREDENTIALS) never trigger a refresh attempt.
	auth?: boolean;
	signal?: AbortSignal;
	// Extra headers beyond Accept/Content-Type/Authorization — added for
	// Payment Service's required Idempotency-Key header
	// (docs/09-api-standards.md §2.6); kept generic rather than a
	// payments-only param since other services may need custom headers later.
	headers?: Record<string, string>;
}

async function parseJsonBody(res: Response): Promise<unknown> {
	if (res.status === 204) return null;
	const text = await res.text();
	if (!text) return null;
	try {
		return JSON.parse(text);
	} catch {
		return null;
	}
}

function isErrorBody(body: unknown): body is ApiErrorBody {
	return typeof body === 'object' && body !== null && 'error' in body;
}

async function performFetch(
	baseUrl: string,
	path: string,
	options: RequestOptions
): Promise<Response> {
	const headers = new Headers({ Accept: 'application/json' });
	if (options.body !== undefined) headers.set('Content-Type', 'application/json');
	if (options.auth) {
		const token = getAccessToken();
		if (token) headers.set('Authorization', `Bearer ${token}`);
	}
	if (options.headers) {
		for (const [key, value] of Object.entries(options.headers)) headers.set(key, value);
	}

	try {
		return await fetch(`${baseUrl}${path}`, {
			method: options.method ?? 'GET',
			headers,
			// Required for the browser to send/receive the HttpOnly refresh-token cookie on
			// every call, per docs/10-authentication-authorization.md §2.
			credentials: 'include',
			body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
			signal: options.signal
		});
	} catch {
		throw new ApiError(
			0,
			NETWORK_ERROR_CODE,
			'Unable to reach the server. Please check your connection and try again.'
		);
	}
}

export async function apiRequest<T>(
	baseUrl: string,
	path: string,
	options: RequestOptions = {}
): Promise<T> {
	return doRequest<T>(baseUrl, path, options, false);
}

async function doRequest<T>(
	baseUrl: string,
	path: string,
	options: RequestOptions,
	isRetry: boolean
): Promise<T> {
	const res = await performFetch(baseUrl, path, options);
	const body = await parseJsonBody(res);

	if (res.ok) {
		if (body === null) return undefined as T;
		return (body as ApiSuccessBody<T>).data;
	}

	const errorDetail = isErrorBody(body) ? body.error : null;

	if (res.status === 401 && options.auth && !isRetry && refreshHandler) {
		const newToken = await refreshHandler().catch(() => null);
		if (newToken) {
			return doRequest<T>(baseUrl, path, options, true);
		}
	}

	throw new ApiError(
		res.status,
		errorDetail?.code ?? 'UNKNOWN_ERROR',
		errorDetail?.message ?? 'Something went wrong. Please try again.',
		errorDetail?.details,
		errorDetail?.requestId
	);
}

export interface DownloadedFile {
	blob: Blob;
	filename: string;
}

// Downloads a file through the same authenticated, refresh-on-401 path as JSON calls. Reports
// are never exposed by a public or pre-signed URL (docs/23 D-R2), so the browser has to fetch
// them with the bearer token and then hand the bytes to the user.
export async function apiDownload(
	baseUrl: string,
	path: string,
	isRetry = false
): Promise<DownloadedFile> {
	const res = await performFetch(baseUrl, path, { auth: true, headers: { Accept: 'text/csv' } });
	if (res.ok) {
		// The server names the file; keep only characters that are safe in a file name.
		const header = res.headers.get('Content-Disposition') ?? '';
		const raw = /filename="([^"]+)"/.exec(header)?.[1] ?? 'report.csv';
		return { blob: await res.blob(), filename: raw.replace(/[^A-Za-z0-9._-]/g, '_') };
	}
	if (res.status === 401 && !isRetry && refreshHandler) {
		const newToken = await refreshHandler().catch(() => null);
		if (newToken) return apiDownload(baseUrl, path, true);
	}
	const body = await parseJsonBody(res);
	const detail = isErrorBody(body) ? body.error : null;
	throw new ApiError(
		res.status,
		detail?.code ?? 'UNKNOWN_ERROR',
		detail?.message ?? 'Something went wrong. Please try again.',
		detail?.details,
		detail?.requestId
	);
}
