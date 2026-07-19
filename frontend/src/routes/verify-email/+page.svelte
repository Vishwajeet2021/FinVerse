<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { verifyEmail } from '$lib/api/auth';
	import { Alert, Card } from '$lib/components/ui';
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

<div class="flex min-h-screen items-center justify-center px-4 py-10">
	<Card class="w-full max-w-md text-center">
		{#if status === 'verifying'}
			<p class="text-sm text-slate-600 dark:text-slate-400">Verifying your email…</p>
		{:else if status === 'success'}
			<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">Email verified</h1>
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
			class="mt-6 inline-block rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
		>
			Go to login
		</a>
	</Card>
</div>
