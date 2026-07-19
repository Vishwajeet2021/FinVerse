<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { getMyProfile } from '$lib/api/customers';
	import { ApiError } from '$lib/api/http';
	import { Alert, Button, Card } from '$lib/components/ui';
	import { logoutAndClearSession } from '$lib/stores/auth';
	import { describeError } from '$lib/utils/errors';

	const profileQuery = createQuery(() => ({
		queryKey: ['customer', 'me'],
		queryFn: getMyProfile,
		retry: false
	}));

	let loggingOut = $state(false);

	async function handleLogout() {
		loggingOut = true;
		try {
			await logoutAndClearSession();
		} catch {
			// Session is cleared locally regardless (stores/auth.ts) — a failed server-side
			// logout call shouldn't trap the user on a page that still thinks they're signed in.
		} finally {
			await goto(resolve('/login'));
		}
	}

	// Customer Service may return 404 for a legitimately-authenticated user who simply hasn't
	// completed onboarding yet — that's a distinct, expected state from "the service is down"
	// or "something broke", per the task's "no profile yet" vs. error-state requirement.
	function isProfileMissing(err: unknown): boolean {
		return err instanceof ApiError && err.status === 404;
	}
</script>

<div class="min-h-screen">
	<header
		class="flex items-center justify-between border-b border-slate-200 bg-white px-6 py-4 dark:border-slate-800 dark:bg-slate-900"
	>
		<h1 class="text-lg font-semibold text-slate-900 dark:text-slate-100">FinVerse</h1>
		<Button variant="secondary" onclick={handleLogout} loading={loggingOut}>Log out</Button>
	</header>

	<main class="mx-auto max-w-2xl px-4 py-10">
		<Card>
			{#if profileQuery.isPending}
				<div class="space-y-3" aria-busy="true" aria-label="Loading profile">
					<div class="h-4 w-1/3 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
					<div class="h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
					<div class="h-4 w-1/4 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				</div>
			{:else if profileQuery.isError}
				{#if isProfileMissing(profileQuery.error)}
					<div class="text-center">
						<h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
							Profile not set up yet
						</h2>
						<p class="mt-2 text-sm text-slate-600 dark:text-slate-400">
							We don't have a customer profile for your account yet. This is expected if onboarding
							hasn't finished, or if Customer Service hasn't created your profile.
						</p>
					</div>
				{:else}
					<Alert
						variant="error"
						message={describeError(
							profileQuery.error,
							"We couldn't reach the customer service right now. Please try again shortly."
						)}
					/>
				{/if}
			{:else if profileQuery.data}
				<dl class="grid grid-cols-1 gap-4 sm:grid-cols-2">
					<div>
						<dt class="text-sm text-slate-500 dark:text-slate-400">Name</dt>
						<dd class="text-base font-medium text-slate-900 dark:text-slate-100">
							{profileQuery.data.fullName}
						</dd>
					</div>
					<div>
						<dt class="text-sm text-slate-500 dark:text-slate-400">KYC tier</dt>
						<dd class="text-base font-medium text-slate-900 dark:text-slate-100">
							{profileQuery.data.kycTier}
						</dd>
					</div>
					<div>
						<dt class="text-sm text-slate-500 dark:text-slate-400">KYC status</dt>
						<dd class="text-base font-medium text-slate-900 dark:text-slate-100">
							{profileQuery.data.kycStatus}
						</dd>
					</div>
				</dl>
			{/if}
		</Card>
	</main>
</div>
