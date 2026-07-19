<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import { onMount } from 'svelte';
	import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
	import { ensureAuthInitialized } from '$lib/stores/auth';

	let { children } = $props();

	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				retry: false,
				refetchOnWindowFocus: false
			}
		}
	});

	onMount(() => {
		// Silently restores a session from the HttpOnly refresh cookie so a page reload
		// doesn't force re-login (docs/10-authentication-authorization.md §2). Runs only in
		// the browser (onMount never fires during SSR) — exactly where the cookie is available.
		void ensureAuthInitialized();
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<QueryClientProvider client={queryClient}>
	{@render children()}
</QueryClientProvider>
