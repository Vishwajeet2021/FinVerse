import type { Handle } from '@sveltejs/kit';

// Response headers that are not part of the Content Security Policy (configured in
// vite.config.ts). HSTS is intentionally absent: it only applies over TLS and is set at the
// TLS-terminating edge (CloudFront) in deployment (docs/22).
export const handle: Handle = async ({ event, resolve }) => {
	const response = await resolve(event);
	response.headers.set('X-Content-Type-Options', 'nosniff');
	response.headers.set('X-Frame-Options', 'DENY');
	response.headers.set('Referrer-Policy', 'strict-origin-when-cross-origin');
	response.headers.set(
		'Permissions-Policy',
		'camera=(), microphone=(), geolocation=(), payment=(), usb=()'
	);
	response.headers.set('Cross-Origin-Opener-Policy', 'same-origin');
	return response;
};
