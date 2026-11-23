<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { verifyEmail } from '$lib/api/auth';
	import { Alert, Card, Logo } from '$lib/components/ui';
	import { describeError } from '$lib/utils/errors';

	type Status = 'verifying' | 'success' | 'error';

	let status = $state<Status>('verifying');
	let errorMessage = $state('');

	onMount(async () => {
		const token = page.url.searchParams.get('token');
		if (!token) {
			status = 'error';
			errorMessage =
				'This verification link is missing its token. Please use the link from your email.';
			return;
		}

		try {
			await verifyEmail(token);
			status = 'success';
		} catch (err) {
			status = 'error';
			errorMessage = describeError(err);
		}
	});
</script>

<div class="auth-backdrop flex min-h-screen flex-col items-center justify-center px-4 py-10">
	<a href={resolve('/')} class="mb-6"><Logo /></a>
	<Card class="w-full max-w-md text-center">
		{#if status === 'verifying'}
			<div class="flex flex-col items-center gap-3 py-2">
				<span
					class="h-6 w-6 animate-spin rounded-full border-2 border-blue-600 border-t-transparent"
					aria-hidden="true"
				></span>
				<p class="text-sm text-slate-600 dark:text-slate-400">Verifying your email…</p>
			</div>
		{:else if status === 'success'}
			<div
				class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-green-100 text-green-600 dark:bg-green-950 dark:text-green-400"
				aria-hidden="true"
			>
				<svg class="h-6 w-6" viewBox="0 0 20 20" fill="currentColor">
					<path
						fill-rule="evenodd"
						d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z"
						clip-rule="evenodd"
					/>
				</svg>
			</div>
			<h1 class="mt-4 text-xl font-semibold text-slate-900 dark:text-slate-100">Email verified</h1>
			<p class="mt-3 text-sm text-slate-600 dark:text-slate-400">
				Your email address has been verified. You can now log in.
			</p>
		{:else}
			<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">Verification failed</h1>
			<div class="mt-3">
				<Alert variant="error" message={errorMessage} />
			</div>
		{/if}

		<a
			href={resolve('/login')}
			class="mt-6 inline-block rounded-lg bg-gradient-to-b from-blue-600 to-blue-700 px-4 py-2 text-sm font-medium text-white shadow-sm shadow-blue-600/30 transition-all hover:from-blue-500 hover:to-blue-600 active:scale-[0.98]"
		>
			Go to login
		</a>
	</Card>
</div>
