<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { listMyAccounts } from '$lib/api/accounts';
	import {
		cancelScheduledPayment,
		cancelStandingInstruction,
		createScheduledPayment,
		createStandingInstruction,
		listScheduledPayments,
		listStandingInstructions,
		pauseStandingInstruction,
		resumeStandingInstruction,
		type Frequency,
		type Rail
	} from '$lib/api/payments';
	import { Alert, Button, Card, Logo } from '$lib/components/ui';
	import { describeError } from '$lib/utils/errors';
	import { formatMinorAsRupees, rupeesToMinor } from '$lib/utils/currency';
	import { RAIL_OPTIONS, formatWhen, localInputToIso } from '$lib/utils/payments';

	const queryClient = useQueryClient();

	const accountsQuery = createQuery(() => ({
		queryKey: ['accounts'],
		queryFn: listMyAccounts,
		retry: false
	}));
	const scheduledQuery = createQuery(() => ({
		queryKey: ['scheduled'],
		queryFn: listScheduledPayments,
		retry: false
	}));
	const standingQuery = createQuery(() => ({
		queryKey: ['standing'],
		queryFn: listStandingInstructions,
		retry: false
	}));

	const activeAccounts = $derived((accountsQuery.data ?? []).filter((a) => a.status === 'ACTIVE'));

	let fromAccountId = $state('');
	$effect(() => {
		if (!fromAccountId && activeAccounts.length > 0) fromAccountId = activeAccounts[0].id;
	});

	let toAccountNumber = $state('');
	let amount = $state('');
	let rail = $state<Rail>('INTERNAL');
	let kind = $state<'once' | 'recurring'>('once');
	let runAt = $state('');
	let frequency = $state<Frequency>('MONTHLY');

	let saving = $state(false);
	let formError = $state<string | null>(null);
	let formSuccess = $state<string | null>(null);

	async function handleCreate(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		formSuccess = null;

		const amountMinor = rupeesToMinor(amount);
		if (amountMinor === null) {
			formError = 'Enter a valid amount greater than zero.';
			return;
		}
		if (!fromAccountId || !toAccountNumber.trim()) {
			formError = 'Choose a source account and enter a destination account number.';
			return;
		}
		const when = localInputToIso(runAt);
		if (!when || new Date(when).getTime() <= Date.now()) {
			formError = 'Pick a date and time in the future.';
			return;
		}

		saving = true;
		try {
			const base = {
				fromAccountId,
				toAccountNumber: toAccountNumber.trim(),
				amountMinor,
				rail
			};
			if (kind === 'once') {
				await createScheduledPayment({ ...base, runAt: when });
				await queryClient.invalidateQueries({ queryKey: ['scheduled'] });
				formSuccess = 'Payment scheduled.';
			} else {
				await createStandingInstruction({ ...base, frequency, firstRunAt: when });
				await queryClient.invalidateQueries({ queryKey: ['standing'] });
				formSuccess = 'Standing instruction created.';
			}
			toAccountNumber = '';
			amount = '';
		} catch (err) {
			formError = describeError(err, "Couldn't save that. Please try again.");
		} finally {
			saving = false;
		}
	}

	let actionError = $state<string | null>(null);
	async function act(fn: () => Promise<void>, queryKey: string) {
		actionError = null;
		try {
			await fn();
		} catch (err) {
			actionError = describeError(err, "Couldn't complete that action. Please try again.");
		} finally {
			await queryClient.invalidateQueries({ queryKey: [queryKey] });
		}
	}

	const statusPill: Record<string, string> = {
		PENDING: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400',
		ACTIVE: 'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400',
		EXECUTED: 'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400',
		PAUSED: 'bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-400',
		CANCELLED: 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400'
	};
	const inputClass =
		'rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100';
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
			<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Schedule a payment</h2>
			<form class="mt-4 grid gap-4 sm:grid-cols-2" onsubmit={handleCreate}>
				<div class="flex flex-col gap-1 sm:col-span-2">
					<span class="text-sm font-medium text-slate-700 dark:text-slate-200">Type</span>
					<div class="flex gap-4 text-sm text-slate-700 dark:text-slate-200">
						<label class="flex items-center gap-2">
							<input type="radio" bind:group={kind} value="once" /> One time
						</label>
						<label class="flex items-center gap-2">
							<input type="radio" bind:group={kind} value="recurring" /> Recurring (standing instruction)
						</label>
					</div>
				</div>

				<div class="flex flex-col gap-1">
					<label for="from" class="text-sm font-medium text-slate-700 dark:text-slate-200">
						From account
					</label>
					<select id="from" bind:value={fromAccountId} class={inputClass}>
						{#each activeAccounts as account (account.id)}
							<option value={account.id}>{account.productType} · {account.accountNumber}</option>
						{/each}
					</select>
				</div>
				<div class="flex flex-col gap-1">
					<label for="to" class="text-sm font-medium text-slate-700 dark:text-slate-200">
						To account number
					</label>
					<input id="to" bind:value={toAccountNumber} class={inputClass} />
				</div>
				<div class="flex flex-col gap-1">
					<label for="amt" class="text-sm font-medium text-slate-700 dark:text-slate-200">
						Amount (₹)
					</label>
					<input
						id="amt"
						inputmode="decimal"
						placeholder="0.00"
						bind:value={amount}
						class={inputClass}
					/>
				</div>
				<div class="flex flex-col gap-1">
					<label for="rail" class="text-sm font-medium text-slate-700 dark:text-slate-200">
						Payment method
					</label>
					<select id="rail" bind:value={rail} class={inputClass}>
						{#each RAIL_OPTIONS as option (option.value)}
							<option value={option.value}>{option.label}</option>
						{/each}
					</select>
				</div>
				<div class="flex flex-col gap-1">
					<label for="when" class="text-sm font-medium text-slate-700 dark:text-slate-200">
						{kind === 'once' ? 'Run at' : 'First run at'}
					</label>
					<input id="when" type="datetime-local" bind:value={runAt} class={inputClass} />
				</div>
				{#if kind === 'recurring'}
					<div class="flex flex-col gap-1">
						<label for="freq" class="text-sm font-medium text-slate-700 dark:text-slate-200">
							Repeats
						</label>
						<select id="freq" bind:value={frequency} class={inputClass}>
							<option value="DAILY">Daily</option>
							<option value="WEEKLY">Weekly</option>
							<option value="MONTHLY">Monthly</option>
						</select>
					</div>
				{/if}
				<div class="sm:col-span-2">
					<Button type="submit" loading={saving}>
						{kind === 'once' ? 'Schedule payment' : 'Create standing instruction'}
					</Button>
				</div>
			</form>
			{#if formError}<div class="mt-3"><Alert variant="error" message={formError} /></div>{/if}
			{#if formSuccess}<div class="mt-3">
					<Alert variant="success" message={formSuccess} />
				</div>{/if}
		</Card>

		{#if actionError}<Alert variant="error" message={actionError} />{/if}

		<Card>
			<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Scheduled payments</h2>
			<div class="mt-4">
				{#if scheduledQuery.isPending}
					<div class="h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				{:else if scheduledQuery.isError}
					<Alert
						variant="error"
						message={describeError(scheduledQuery.error, "Couldn't load scheduled payments.")}
					/>
				{:else if (scheduledQuery.data ?? []).length === 0}
					<p class="text-sm text-slate-600 dark:text-slate-400">Nothing scheduled.</p>
				{:else}
					<ul class="divide-y divide-slate-100 dark:divide-slate-800">
						{#each scheduledQuery.data ?? [] as item (item.id)}
							<li class="flex items-center gap-3 py-3">
								<div class="min-w-0 flex-1">
									<p class="text-sm font-medium text-slate-900 dark:text-slate-100">
										₹{formatMinorAsRupees(item.amountMinor)} to {item.toReference} · {item.rail}
									</p>
									<p class="text-xs text-slate-500 dark:text-slate-400">{formatWhen(item.runAt)}</p>
								</div>
								<span
									class="rounded-full px-2.5 py-0.5 text-xs font-medium {statusPill[item.status] ??
										statusPill.CANCELLED}"
								>
									{item.status}
								</span>
								{#if item.status === 'PENDING'}
									<Button
										variant="secondary"
										onclick={() => act(() => cancelScheduledPayment(item.id), 'scheduled')}
									>
										Cancel
									</Button>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</Card>

		<Card>
			<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
				Standing instructions
			</h2>
			<div class="mt-4">
				{#if standingQuery.isPending}
					<div class="h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				{:else if standingQuery.isError}
					<Alert
						variant="error"
						message={describeError(standingQuery.error, "Couldn't load standing instructions.")}
					/>
				{:else if (standingQuery.data ?? []).length === 0}
					<p class="text-sm text-slate-600 dark:text-slate-400">No recurring payments set up.</p>
				{:else}
					<ul class="divide-y divide-slate-100 dark:divide-slate-800">
						{#each standingQuery.data ?? [] as item (item.id)}
							<li class="flex flex-wrap items-center gap-3 py-3">
								<div class="min-w-0 flex-1">
									<p class="text-sm font-medium text-slate-900 dark:text-slate-100">
										₹{formatMinorAsRupees(item.amountMinor)} to {item.toReference} · {item.rail} · {item.frequency.toLowerCase()}
									</p>
									<p class="text-xs text-slate-500 dark:text-slate-400">
										Next run {formatWhen(item.nextRunAt)}
									</p>
								</div>
								<span
									class="rounded-full px-2.5 py-0.5 text-xs font-medium {statusPill[item.status] ??
										statusPill.CANCELLED}"
								>
									{item.status}
								</span>
								{#if item.status === 'ACTIVE'}
									<Button
										variant="secondary"
										onclick={() => act(() => pauseStandingInstruction(item.id), 'standing')}
									>
										Pause
									</Button>
								{:else if item.status === 'PAUSED'}
									<Button
										variant="secondary"
										onclick={() => act(() => resumeStandingInstruction(item.id), 'standing')}
									>
										Resume
									</Button>
								{/if}
								{#if item.status !== 'CANCELLED'}
									<Button
										variant="secondary"
										onclick={() => act(() => cancelStandingInstruction(item.id), 'standing')}
									>
										Cancel
									</Button>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</Card>
	</main>
</div>
