<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import {
		MAX_REASON,
		MIN_REASON,
		freezeAccount,
		getDashboard,
		listActions,
		resolveFraudCase,
		reviewKyc,
		unfreezeAccount,
		type AdminAction,
		type FraudCase
	} from '$lib/api/admin';
	import { requestFraudSummary } from '$lib/api/reports';
	import { ApiError } from '$lib/api/http';
	import { Alert, Button, Card, Logo } from '$lib/components/ui';
	import { describeError } from '$lib/utils/errors';
	import { formatMinorAsRupees } from '$lib/utils/currency';

	const queryClient = useQueryClient();

	// The server decides who is an administrator. The console simply asks for its data; a 403
	// means this user holds no admin scope, and nothing below is shown.
	const dashboardQuery = createQuery(() => ({
		queryKey: ['admin', 'dashboard'],
		queryFn: getDashboard,
		retry: false,
		refetchInterval: 15_000
	}));
	const actionsQuery = createQuery(() => ({
		queryKey: ['admin', 'actions'],
		queryFn: () => listActions(20),
		retry: false
	}));

	const forbidden = $derived(
		dashboardQuery.isError &&
			dashboardQuery.error instanceof ApiError &&
			(dashboardQuery.error.status === 403 || dashboardQuery.error.status === 401)
	);

	const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
	const reasonProblem = (r: string): string | null => {
		const n = [...r.trim()].length;
		return n < MIN_REASON || n > MAX_REASON
			? `Give a reason of ${MIN_REASON} to ${MAX_REASON} characters. It is recorded permanently.`
			: null;
	};

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: ['admin'] });
	}

	// ---- Account freeze / unfreeze -------------------------------------------------------
	let accountId = $state('');
	let accountReason = $state('');
	let accountAction = $state<'freeze' | 'unfreeze' | null>(null); // awaiting confirmation
	let accountBusy = $state(false);
	let accountError = $state<string | null>(null);
	let accountOk = $state<string | null>(null);

	function askAccount(kind: 'freeze' | 'unfreeze') {
		accountError = accountOk = null;
		if (!UUID.test(accountId.trim())) {
			accountError = 'Enter the account id (a UUID).';
			return;
		}
		const problem = reasonProblem(accountReason);
		if (problem) {
			accountError = problem;
			return;
		}
		accountAction = kind;
	}

	async function confirmAccount() {
		if (!accountAction) return;
		accountBusy = true;
		accountError = accountOk = null;
		try {
			const run = accountAction === 'freeze' ? freezeAccount : unfreezeAccount;
			await run(accountId.trim(), accountReason.trim());
			accountOk = `Account ${accountAction === 'freeze' ? 'frozen' : 'unfrozen'}.`;
			accountReason = '';
			await refresh();
		} catch (err) {
			accountError = describeError(err, "Couldn't complete that action.");
		} finally {
			accountBusy = false;
			accountAction = null;
		}
	}

	// ---- Fraud case resolution -----------------------------------------------------------
	let caseId = $state<string | null>(null);
	let caseOutcome = $state<'CONFIRMED' | 'DISMISSED'>('DISMISSED');
	let caseReason = $state('');
	let caseBusy = $state(false);
	let caseError = $state<string | null>(null);
	let caseOk = $state<string | null>(null);

	function openCase(c: FraudCase) {
		caseId = c.id;
		caseReason = '';
		caseError = caseOk = null;
	}

	async function submitCase(event: SubmitEvent) {
		event.preventDefault();
		if (!caseId) return;
		const problem = reasonProblem(caseReason);
		if (problem) {
			caseError = problem;
			return;
		}
		caseBusy = true;
		caseError = caseOk = null;
		try {
			await resolveFraudCase(caseId, caseOutcome, caseReason.trim());
			caseOk = `Case marked ${caseOutcome.toLowerCase()}.`;
			caseId = null;
			await refresh();
		} catch (err) {
			caseError = describeError(err, "Couldn't resolve that case.");
		} finally {
			caseBusy = false;
		}
	}

	// ---- KYC review ----------------------------------------------------------------------
	let kycCustomer = $state('');
	let kycApprove = $state(true);
	let kycTier = $state(1);
	let kycReason = $state('');
	let kycBusy = $state(false);
	let kycError = $state<string | null>(null);
	let kycOk = $state<string | null>(null);

	async function submitKyc(event: SubmitEvent) {
		event.preventDefault();
		kycError = kycOk = null;
		if (!UUID.test(kycCustomer.trim())) {
			kycError = 'Enter the customer id (a UUID).';
			return;
		}
		const problem = reasonProblem(kycReason);
		if (problem) {
			kycError = problem;
			return;
		}
		kycBusy = true;
		try {
			await reviewKyc(kycCustomer.trim(), kycApprove, kycApprove ? kycTier : 0, kycReason.trim());
			kycOk = kycApprove ? `Approved at tier ${kycTier}.` : 'Rejected.';
			kycReason = '';
			await refresh();
		} catch (err) {
			kycError = describeError(err, "Couldn't record that review.");
		} finally {
			kycBusy = false;
		}
	}

	// ---- Operational report --------------------------------------------------------------
	let summaryBusy = $state(false);
	let summaryMsg = $state<string | null>(null);
	let summaryError = $state<string | null>(null);
	async function requestSummary() {
		summaryBusy = true;
		summaryMsg = summaryError = null;
		try {
			await requestFraudSummary();
			summaryMsg = 'Report requested. Download it from the reports page when it is ready.';
		} catch (err) {
			summaryError = describeError(err, "Couldn't request the report.");
		} finally {
			summaryBusy = false;
		}
	}

	const actionLabel: Record<AdminAction['type'], string> = {
		ACCOUNT_FREEZE: 'Freeze account',
		ACCOUNT_UNFREEZE: 'Unfreeze account',
		FRAUD_CASE_RESOLVE: 'Resolve fraud case',
		KYC_REVIEW: 'KYC review'
	};
	const pill: Record<string, string> = {
		SUCCEEDED: 'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400',
		FAILED: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-400',
		REQUESTED: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400'
	};
	const inputClass =
		'w-full rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100';
	const labelClass = 'flex flex-col gap-1 text-xs font-medium text-slate-600 dark:text-slate-400';
	const when = (iso: string) => new Date(iso).toLocaleString('en-IN');
</script>

{#snippet stat(label: string, value: string | number, warn = false)}
	<div>
		<dt class="text-xs font-medium tracking-wide text-slate-500 uppercase dark:text-slate-400">
			{label}
		</dt>
		<dd
			class="mt-1 text-2xl font-semibold {warn
				? 'text-red-600 dark:text-red-400'
				: 'text-slate-900 dark:text-slate-100'}"
		>
			{value}
		</dd>
	</div>
{/snippet}

{#snippet unavailable()}
	<p class="mt-3 text-sm text-slate-500 dark:text-slate-400">
		Unavailable right now. The source did not answer.
	</p>
{/snippet}

<div class="min-h-screen">
	<header
		class="sticky top-0 z-10 flex items-center justify-between border-b border-slate-200/80 bg-white/80 px-6 py-3.5 backdrop-blur-sm dark:border-slate-800 dark:bg-slate-900/80"
	>
		<div class="flex items-center gap-3">
			<Logo size="sm" />
			<span class="text-sm font-medium text-slate-500 dark:text-slate-400">Admin console</span>
		</div>
		<div class="flex items-center gap-2">
			<Button variant="secondary" onclick={() => goto(resolve('/statements'))}>Reports</Button>
			<Button variant="secondary" onclick={() => goto(resolve('/dashboard'))}>Dashboard</Button>
		</div>
	</header>

	<main class="mx-auto max-w-5xl space-y-6 px-4 py-10">
		{#if dashboardQuery.isPending}
			<div class="h-24 animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800"></div>
		{:else if forbidden}
			<Alert variant="error" message="You don't have access to the admin console." />
		{:else if dashboardQuery.isError}
			<Alert
				variant="error"
				message={describeError(
					dashboardQuery.error,
					"The admin service isn't reachable right now."
				)}
			/>
		{:else}
			{@const d = dashboardQuery.data}
			<section class="grid gap-4 md:grid-cols-3" aria-label="Overview">
				<Card>
					<h2 class="text-sm font-semibold text-slate-900 dark:text-slate-100">Fraud</h2>
					{#if d.fraud.available && d.fraud.data}
						<dl class="mt-3">
							{@render stat(
								'Open cases',
								d.fraud.data.openCases >= 200 ? '200+' : d.fraud.data.openCases,
								d.fraud.data.openCases > 0
							)}
						</dl>
					{:else}
						{@render unavailable()}
					{/if}
				</Card>
				<Card>
					<h2 class="text-sm font-semibold text-slate-900 dark:text-slate-100">
						Payments, last hour
					</h2>
					{#if d.payments.available && d.payments.data}
						<dl class="mt-3 grid grid-cols-3 gap-2">
							{@render stat('Started', d.payments.data.initiated1h)}
							{@render stat('Done', d.payments.data.completed1h)}
							{@render stat('Failed', d.payments.data.failed1h, d.payments.data.failed1h > 0)}
						</dl>
					{:else}
						{@render unavailable()}
					{/if}
				</Card>
				<Card>
					<h2 class="text-sm font-semibold text-slate-900 dark:text-slate-100">Operations</h2>
					{#if d.ops.available && d.ops.data}
						<dl class="mt-3 grid grid-cols-2 gap-2">
							{@render stat(
								'Dead letters',
								d.ops.data.deadLetterDepth,
								d.ops.data.deadLetterDepth > 0
							)}
							{@render stat('Services down', d.ops.data.servicesDown, d.ops.data.servicesDown > 0)}
						</dl>
						{#if d.ops.data.firingAlerts.length > 0}
							<p class="mt-3 text-xs text-red-600 dark:text-red-400">
								Firing: {d.ops.data.firingAlerts.join(', ')}
							</p>
						{/if}
					{:else}
						{@render unavailable()}
					{/if}
				</Card>
			</section>
			<p class="text-xs text-slate-500 dark:text-slate-400">
				Updated {when(d.generatedAt)}. Active customers is not shown: no service exposes that count
				yet.
			</p>

			<Card>
				<div class="flex flex-wrap items-center justify-between gap-3">
					<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
						Open fraud cases
					</h2>
					<Button variant="secondary" loading={summaryBusy} onclick={requestSummary}
						>Export summary (CSV)</Button
					>
				</div>
				{#if summaryMsg}<div class="mt-3">
						<Alert variant="success" message={summaryMsg} />
					</div>{/if}
				{#if summaryError}<div class="mt-3">
						<Alert variant="error" message={summaryError} />
					</div>{/if}
				{#if caseOk}<div class="mt-3"><Alert variant="success" message={caseOk} /></div>{/if}

				{#if !d.fraud.available || !d.fraud.data}
					{@render unavailable()}
				{:else if d.fraud.data.recent.length === 0}
					<p class="mt-3 text-sm text-slate-500 dark:text-slate-400">No open cases.</p>
				{:else}
					<ul class="mt-3 divide-y divide-slate-200 dark:divide-slate-800">
						{#each d.fraud.data.recent as c (c.id)}
							<li class="py-3">
								<div class="flex flex-wrap items-center justify-between gap-3">
									<div class="min-w-0 text-sm">
										<p class="font-medium text-slate-900 dark:text-slate-100">
											₹{formatMinorAsRupees(c.amountMinor)} · {c.rule} · risk {c.riskScore}
										</p>
										<p class="text-xs text-slate-500 dark:text-slate-400">
											{c.reason} · account {c.accountId.slice(0, 8)} · {when(c.createdAt)}
										</p>
									</div>
									<Button variant="secondary" onclick={() => openCase(c)}>Review</Button>
								</div>
								{#if caseId === c.id}
									<form class="mt-3 grid gap-3 sm:grid-cols-3" onsubmit={submitCase}>
										<label class={labelClass}>
											Outcome
											<select bind:value={caseOutcome} class={inputClass}>
												<option value="DISMISSED">Dismiss (legitimate)</option>
												<option value="CONFIRMED">Confirm fraud</option>
											</select>
										</label>
										<label class="{labelClass} sm:col-span-2">
											Reason (recorded permanently)
											<input
												bind:value={caseReason}
												maxlength={MAX_REASON}
												class={inputClass}
												autocomplete="off"
											/>
										</label>
										<div class="flex gap-2 sm:col-span-3">
											<Button type="submit" loading={caseBusy}>Resolve case</Button>
											<Button variant="secondary" onclick={() => (caseId = null)}>Cancel</Button>
										</div>
										{#if caseError}
											<div class="sm:col-span-3"><Alert variant="error" message={caseError} /></div>
										{/if}
									</form>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</Card>

			<div class="grid gap-6 md:grid-cols-2">
				<Card>
					<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
						Freeze or unfreeze
					</h2>
					<p class="mt-1 text-sm text-slate-600 dark:text-slate-400">
						A freeze placed here can only be lifted by an administrator.
					</p>
					<div class="mt-4 space-y-3">
						<label class={labelClass}>
							Account id
							<input
								bind:value={accountId}
								class={inputClass}
								placeholder="00000000-0000-0000-0000-000000000000"
								autocomplete="off"
								spellcheck="false"
							/>
						</label>
						<label class={labelClass}>
							Reason (recorded permanently)
							<input
								bind:value={accountReason}
								maxlength={MAX_REASON}
								class={inputClass}
								autocomplete="off"
							/>
						</label>
						{#if accountAction}
							<Alert
								variant="info"
								message={`You are about to ${accountAction} account ${accountId.trim()}. This is recorded under your name.`}
							/>
							<div class="flex gap-2">
								<Button
									variant={accountAction === 'freeze' ? 'danger' : 'primary'}
									loading={accountBusy}
									onclick={confirmAccount}>Confirm {accountAction}</Button
								>
								<Button variant="secondary" onclick={() => (accountAction = null)}>Cancel</Button>
							</div>
						{:else}
							<div class="flex gap-2">
								<Button variant="danger" onclick={() => askAccount('freeze')}>Freeze</Button>
								<Button variant="secondary" onclick={() => askAccount('unfreeze')}>Unfreeze</Button>
							</div>
						{/if}
						{#if accountError}<Alert variant="error" message={accountError} />{/if}
						{#if accountOk}<Alert variant="success" message={accountOk} />{/if}
					</div>
				</Card>

				<Card>
					<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">KYC review</h2>
					<form class="mt-4 space-y-3" onsubmit={submitKyc}>
						<label class={labelClass}>
							Customer id
							<input
								bind:value={kycCustomer}
								class={inputClass}
								placeholder="00000000-0000-0000-0000-000000000000"
								autocomplete="off"
								spellcheck="false"
							/>
						</label>
						<div class="grid grid-cols-2 gap-3">
							<label class={labelClass}>
								Decision
								<select bind:value={kycApprove} class={inputClass}>
									<option value={true}>Approve</option>
									<option value={false}>Reject</option>
								</select>
							</label>
							<label class={labelClass}>
								Tier
								<select bind:value={kycTier} disabled={!kycApprove} class={inputClass}>
									<option value={1}>1</option>
									<option value={2}>2</option>
									<option value={3}>3</option>
								</select>
							</label>
						</div>
						<label class={labelClass}>
							Reason (recorded permanently)
							<input
								bind:value={kycReason}
								maxlength={MAX_REASON}
								class={inputClass}
								autocomplete="off"
							/>
						</label>
						<Button type="submit" loading={kycBusy}>Record review</Button>
						{#if kycError}<Alert variant="error" message={kycError} />{/if}
						{#if kycOk}<Alert variant="success" message={kycOk} />{/if}
					</form>
				</Card>
			</div>

			<Card>
				<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
					Recent administrative actions
				</h2>
				{#if actionsQuery.isPending}
					<div class="mt-3 h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				{:else if actionsQuery.isError}
					<div class="mt-3">
						<Alert
							variant="error"
							message={describeError(actionsQuery.error, "Couldn't load the action log.")}
						/>
					</div>
				{:else if actionsQuery.data.length === 0}
					<p class="mt-3 text-sm text-slate-500 dark:text-slate-400">Nothing recorded yet.</p>
				{:else}
					<ul class="mt-3 divide-y divide-slate-200 dark:divide-slate-800">
						{#each actionsQuery.data as a (a.id)}
							<li class="flex flex-wrap items-start justify-between gap-3 py-3 text-sm">
								<div class="min-w-0">
									<p class="font-medium text-slate-900 dark:text-slate-100">
										{actionLabel[a.type] ?? a.type} · {a.targetId.slice(0, 8)}
									</p>
									<p class="text-xs text-slate-500 dark:text-slate-400">
										{when(a.createdAt)} · by {a.actorId.slice(0, 8)} · {a.reason}
									</p>
									{#if a.failureReason}
										<p class="text-xs text-red-600 dark:text-red-400">{a.failureReason}</p>
									{/if}
								</div>
								<span
									class="inline-flex shrink-0 items-center rounded-full px-2.5 py-0.5 text-xs font-medium {pill[
										a.status
									] ?? ''}">{a.status}</span
								>
							</li>
						{/each}
					</ul>
				{/if}
			</Card>
		{/if}
	</main>
</div>
