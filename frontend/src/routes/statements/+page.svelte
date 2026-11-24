<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { listMyAccounts } from '$lib/api/accounts';
	import {
		describeReportFailure,
		downloadReport,
		listReports,
		requestStatement,
		type Report
	} from '$lib/api/reports';
	import { Alert, Button, Card, Logo } from '$lib/components/ui';
	import { describeError } from '$lib/utils/errors';

	const queryClient = useQueryClient();

	const accountsQuery = createQuery(() => ({
		queryKey: ['accounts'],
		queryFn: listMyAccounts,
		retry: false
	}));

	// Reports are built in the background; poll quickly while any is unfinished, then stop.
	const reportsQuery = createQuery(() => ({
		queryKey: ['reports'],
		queryFn: listReports,
		retry: false,
		refetchInterval: (query) =>
			query.state.data?.some((r) => r.status === 'QUEUED' || r.status === 'RUNNING') ? 2000 : false
	}));

	// Statement periods are calendar dates in UTC, which is what the server enforces.
	const isoDay = (d: Date) => d.toISOString().slice(0, 10);
	const todayUtc = isoDay(new Date());
	const thirtyDaysAgo = isoDay(new Date(Date.now() - 30 * 86_400_000));

	let accountId = $state('');
	$effect(() => {
		if (!accountId && accountsQuery.data && accountsQuery.data.length > 0) {
			accountId = accountsQuery.data[0].id;
		}
	});
	let from = $state(thirtyDaysAgo);
	let to = $state(todayUtc);

	let requesting = $state(false);
	let formError = $state<string | null>(null);
	let formInfo = $state<string | null>(null);

	async function handleRequest(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		formInfo = null;
		if (!accountId) {
			formError = 'Choose an account.';
			return;
		}
		if (!from || !to || from > to) {
			formError = 'Choose a start date that is not after the end date.';
			return;
		}
		if (to > todayUtc) {
			formError = 'The end date cannot be in the future.';
			return;
		}
		const days = (Date.parse(to) - Date.parse(from)) / 86_400_000 + 1;
		if (days > 366) {
			formError = 'A statement can cover at most 366 days.';
			return;
		}
		requesting = true;
		try {
			await requestStatement(accountId, from, to);
			formInfo = 'Your statement is being prepared. It will appear below when it is ready.';
			await queryClient.invalidateQueries({ queryKey: ['reports'] });
		} catch (err) {
			formError = describeError(err, "Couldn't request that statement. Please try again.");
		} finally {
			requesting = false;
		}
	}

	let downloadError = $state<string | null>(null);
	let downloadingId = $state<string | null>(null);
	async function handleDownload(report: Report) {
		downloadError = null;
		downloadingId = report.id;
		try {
			const { blob, filename } = await downloadReport(report.id);
			const url = URL.createObjectURL(blob);
			const link = document.createElement('a');
			link.href = url;
			link.download = filename;
			document.body.appendChild(link);
			link.click();
			link.remove();
			URL.revokeObjectURL(url);
		} catch (err) {
			downloadError = describeError(err, "Couldn't download that report. Please try again.");
		} finally {
			downloadingId = null;
		}
	}

	const accountLabel = (id?: string) =>
		accountsQuery.data?.find((a) => a.id === id)?.accountNumber ?? 'Account';

	const statusPill: Record<string, string> = {
		QUEUED: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400',
		RUNNING: 'bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-400',
		READY: 'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400',
		FAILED: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-400',
		EXPIRED: 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400'
	};
	const inputClass =
		'rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100';
	const when = (iso?: string) => (iso ? new Date(iso).toLocaleString('en-IN') : '');
</script>

<div class="min-h-screen">
	<header
		class="sticky top-0 z-10 flex items-center justify-between border-b border-slate-200/80 bg-white/80 px-6 py-3.5 backdrop-blur-sm dark:border-slate-800 dark:bg-slate-900/80"
	>
		<Logo size="sm" />
		<Button variant="secondary" onclick={() => goto(resolve('/dashboard'))}
			>Back to dashboard</Button
		>
	</header>

	<main class="mx-auto max-w-3xl space-y-6 px-4 py-10">
		<Card>
			<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
				Download a statement
			</h2>
			<p class="mt-1 text-sm text-slate-600 dark:text-slate-400">
				A CSV of every transaction in the period, with a running balance. Dates are in UTC and a
				statement covers up to 366 days.
			</p>

			{#if accountsQuery.isError}
				<div class="mt-4">
					<Alert
						variant="error"
						message={describeError(accountsQuery.error, "We couldn't load your accounts.")}
					/>
				</div>
			{:else}
				<form class="mt-4 grid gap-3 sm:grid-cols-3" onsubmit={handleRequest}>
					<label class="flex flex-col gap-1 text-xs font-medium text-slate-600 dark:text-slate-400">
						Account
						<select bind:value={accountId} class={inputClass}>
							{#each accountsQuery.data ?? [] as account (account.id)}
								<option value={account.id}>{account.accountNumber} ({account.productType})</option>
							{/each}
						</select>
					</label>
					<label class="flex flex-col gap-1 text-xs font-medium text-slate-600 dark:text-slate-400">
						From
						<input type="date" bind:value={from} max={todayUtc} class={inputClass} />
					</label>
					<label class="flex flex-col gap-1 text-xs font-medium text-slate-600 dark:text-slate-400">
						To
						<input type="date" bind:value={to} max={todayUtc} class={inputClass} />
					</label>
					<div class="sm:col-span-3">
						<Button type="submit" loading={requesting}>Generate statement</Button>
					</div>
				</form>
			{/if}

			{#if formError}<div class="mt-3"><Alert variant="error" message={formError} /></div>{/if}
			{#if formInfo}<div class="mt-3"><Alert variant="info" message={formInfo} /></div>{/if}
		</Card>

		<Card>
			<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Your reports</h2>
			<p class="mt-1 text-sm text-slate-600 dark:text-slate-400">
				Reports are kept for 7 days and then removed.
			</p>
			{#if downloadError}<div class="mt-3">
					<Alert variant="error" message={downloadError} />
				</div>{/if}

			<div class="mt-4">
				{#if reportsQuery.isPending}
					<div class="h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				{:else if reportsQuery.isError}
					<Alert
						variant="error"
						message={describeError(reportsQuery.error, "We couldn't load your reports.")}
					/>
				{:else if reportsQuery.data.length === 0}
					<p class="text-sm text-slate-500 dark:text-slate-400">No reports yet.</p>
				{:else}
					<ul class="divide-y divide-slate-200 dark:divide-slate-800">
						{#each reportsQuery.data as report (report.id)}
							<li class="flex flex-wrap items-center justify-between gap-3 py-3">
								<div class="min-w-0">
									<p class="text-sm font-medium text-slate-900 dark:text-slate-100">
										{#if report.kind === 'STATEMENT'}
											{accountLabel(report.accountId)} · {report.from?.slice(0, 10)} to {report.to?.slice(
												0,
												10
											)}
										{:else}
											Fraud case summary
										{/if}
									</p>
									<p class="text-xs text-slate-500 dark:text-slate-400">
										Requested {when(report.createdAt)}
										{#if report.status === 'READY'}· {report.rowCount} rows{/if}
									</p>
									{#if report.status === 'FAILED'}
										<p class="mt-1 text-xs text-red-600 dark:text-red-400">
											{describeReportFailure(report.failureCode)}
										</p>
									{/if}
								</div>
								<div class="flex items-center gap-3">
									<span
										class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium {statusPill[
											report.status
										] ?? ''}">{report.status}</span
									>
									{#if report.status === 'READY'}
										<Button
											variant="secondary"
											loading={downloadingId === report.id}
											onclick={() => handleDownload(report)}>Download CSV</Button
										>
									{/if}
								</div>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</Card>
	</main>
</div>
