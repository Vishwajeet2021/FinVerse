<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import {
		getBalance,
		getStatement,
		listMyAccounts,
		openAccount,
		type Account
	} from '$lib/api/accounts';
	import { getMyProfile } from '$lib/api/customers';
	import { initiatePayment } from '$lib/api/payments';
	import { ApiError } from '$lib/api/http';
	import { Alert, Button, Card } from '$lib/components/ui';
	import { logoutAndClearSession } from '$lib/stores/auth';
	import { describeError } from '$lib/utils/errors';
	import { formatMinorAsRupees, rupeesToMinor } from '$lib/utils/currency';

	const queryClient = useQueryClient();

	const profileQuery = createQuery(() => ({
		queryKey: ['customer', 'me'],
		queryFn: getMyProfile,
		retry: false
	}));

	const accountsQuery = createQuery(() => ({
		queryKey: ['accounts'],
		queryFn: listMyAccounts,
		retry: false
	}));

	let selectedAccountId = $state<string | null>(null);
	$effect(() => {
		const accounts = accountsQuery.data;
		if (accounts && accounts.length > 0 && !selectedAccountId) {
			selectedAccountId = accounts[0].id;
		}
	});

	const balanceQuery = createQuery(() => ({
		queryKey: ['balance', selectedAccountId],
		queryFn: () => getBalance(selectedAccountId as string),
		enabled: selectedAccountId !== null
	}));

	const statementQuery = createQuery(() => ({
		queryKey: ['statement', selectedAccountId],
		queryFn: () => getStatement(selectedAccountId as string),
		enabled: selectedAccountId !== null
	}));

	let loggingOut = $state(false);
	async function handleLogout() {
		loggingOut = true;
		try {
			await logoutAndClearSession();
		} catch {
			// stores/auth.ts already clears local session state regardless
		} finally {
			await goto(resolve('/login'));
		}
	}

	let openingAccount = $state(false);
	let openAccountError = $state<string | null>(null);
	async function handleOpenAccount(productType: Account['productType']) {
		openingAccount = true;
		openAccountError = null;
		try {
			const account = await openAccount(productType);
			await queryClient.invalidateQueries({ queryKey: ['accounts'] });
			selectedAccountId = account.id;
		} catch (err) {
			openAccountError = describeError(err, "Couldn't open the account. Please try again.");
		} finally {
			openingAccount = false;
		}
	}

	let toAccountId = $state('');
	let transferAmount = $state('');
	let transferring = $state(false);
	let transferError = $state<string | null>(null);
	let transferSuccess = $state<string | null>(null);

	async function handleTransfer(event: SubmitEvent) {
		event.preventDefault();
		transferError = null;
		transferSuccess = null;

		if (!selectedAccountId) return;
		const amountMinor = rupeesToMinor(transferAmount);
		if (amountMinor === null) {
			transferError = 'Enter a valid amount greater than zero.';
			return;
		}
		if (!toAccountId.trim()) {
			transferError = 'Enter a destination account ID.';
			return;
		}

		transferring = true;
		try {
			const payment = await initiatePayment(selectedAccountId, toAccountId.trim(), amountMinor);
			if (payment.status === 'COMPLETED') {
				transferSuccess = `Transfer completed (transaction ${payment.transactionId}).`;
				toAccountId = '';
				transferAmount = '';
				await queryClient.invalidateQueries({ queryKey: ['balance', selectedAccountId] });
				await queryClient.invalidateQueries({ queryKey: ['statement', selectedAccountId] });
			} else {
				transferError = payment.failureReason ?? 'The transfer did not complete. Please try again.';
			}
		} catch (err) {
			transferError = describeError(err, "Couldn't complete the transfer. Please try again.");
		} finally {
			transferring = false;
		}
	}

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

	<main class="mx-auto max-w-3xl space-y-6 px-4 py-10">
		<Card>
			{#if profileQuery.isPending}
				<div class="h-4 w-1/3 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
			{:else if profileQuery.isError}
				{#if isProfileMissing(profileQuery.error)}
					<p class="text-sm text-slate-600 dark:text-slate-400">
						We don't have a customer profile for your account yet.
					</p>
				{:else}
					<Alert
						variant="error"
						message={describeError(
							profileQuery.error,
							"We couldn't reach the customer service right now."
						)}
					/>
				{/if}
			{:else if profileQuery.data}
				<dl class="grid grid-cols-1 gap-4 sm:grid-cols-3">
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

		<Card>
			<div class="flex items-center justify-between">
				<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Your accounts</h2>
				<div class="flex gap-2">
					<Button
						variant="secondary"
						loading={openingAccount}
						onclick={() => handleOpenAccount('SAVINGS')}
					>
						Open savings
					</Button>
					<Button
						variant="secondary"
						loading={openingAccount}
						onclick={() => handleOpenAccount('CURRENT')}
					>
						Open current
					</Button>
				</div>
			</div>
			{#if openAccountError}
				<div class="mt-3"><Alert variant="error" message={openAccountError} /></div>
			{/if}

			<div class="mt-4">
				{#if accountsQuery.isPending}
					<div class="h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				{:else if accountsQuery.isError}
					<Alert
						variant="error"
						message={describeError(accountsQuery.error, "Couldn't load your accounts.")}
					/>
				{:else if accountsQuery.data && accountsQuery.data.length === 0}
					<p class="text-sm text-slate-600 dark:text-slate-400">
						You don't have any accounts yet — open one above to get started.
					</p>
				{:else if accountsQuery.data}
					<ul class="divide-y divide-slate-200 dark:divide-slate-800">
						{#each accountsQuery.data as account (account.id)}
							<li>
								<button
									type="button"
									onclick={() => (selectedAccountId = account.id)}
									class="flex w-full items-center justify-between px-2 py-3 text-left hover:bg-slate-50 dark:hover:bg-slate-800 {selectedAccountId ===
									account.id
										? 'bg-slate-50 dark:bg-slate-800'
										: ''}"
								>
									<span>
										<span class="block text-sm font-medium text-slate-900 dark:text-slate-100">
											{account.productType} · {account.accountNumber}
										</span>
										<span class="block text-xs text-slate-500 dark:text-slate-400"
											>{account.status}</span
										>
									</span>
									<span class="font-mono text-sm text-slate-700 dark:text-slate-300">
										{account.id === selectedAccountId && balanceQuery.data
											? `₹${formatMinorAsRupees(balanceQuery.data.ledgerBalanceMinor)}`
											: ''}
									</span>
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</Card>

		{#if selectedAccountId}
			<Card>
				<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Transfer money</h2>
				<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
					From account {selectedAccountId}
				</p>

				<form class="mt-4 flex flex-col gap-4 sm:flex-row sm:items-end" onsubmit={handleTransfer}>
					<div class="flex flex-1 flex-col gap-1">
						<label for="toAccountId" class="text-sm font-medium text-slate-700 dark:text-slate-200">
							To account ID
						</label>
						<input
							id="toAccountId"
							type="text"
							bind:value={toAccountId}
							placeholder="destination account ID"
							class="rounded-md border border-slate-300 px-3 py-2 text-sm shadow-sm focus:ring-2 focus:ring-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
						/>
					</div>
					<div class="flex w-40 flex-col gap-1">
						<label for="amount" class="text-sm font-medium text-slate-700 dark:text-slate-200">
							Amount (₹)
						</label>
						<input
							id="amount"
							type="text"
							inputmode="decimal"
							bind:value={transferAmount}
							placeholder="0.00"
							class="rounded-md border border-slate-300 px-3 py-2 text-sm shadow-sm focus:ring-2 focus:ring-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
						/>
					</div>
					<Button type="submit" loading={transferring}>Send</Button>
				</form>

				{#if transferError}
					<div class="mt-3"><Alert variant="error" message={transferError} /></div>
				{/if}
				{#if transferSuccess}
					<div class="mt-3"><Alert variant="success" message={transferSuccess} /></div>
				{/if}
			</Card>

			<Card>
				<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
					Transaction history
				</h2>
				<div class="mt-4">
					{#if statementQuery.isPending}
						<div class="h-4 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
					{:else if statementQuery.isError}
						<Alert
							variant="error"
							message={describeError(statementQuery.error, "Couldn't load transaction history.")}
						/>
					{:else if statementQuery.data && statementQuery.data.length === 0}
						<p class="text-sm text-slate-600 dark:text-slate-400">No transactions yet.</p>
					{:else if statementQuery.data}
						<div class="overflow-x-auto">
							<table class="w-full text-sm">
								<thead>
									<tr
										class="border-b border-slate-200 text-left text-slate-500 dark:border-slate-800 dark:text-slate-400"
									>
										<th class="py-2 pr-4 font-medium">Transaction</th>
										<th class="py-2 pr-4 font-medium">Direction</th>
										<th class="py-2 font-medium">Amount</th>
									</tr>
								</thead>
								<tbody>
									{#each statementQuery.data as entry (entry.legId)}
										<tr class="border-b border-slate-100 dark:border-slate-800">
											<td class="py-2 pr-4 font-mono text-xs text-slate-600 dark:text-slate-400">
												{entry.transactionId}
											</td>
											<td class="py-2 pr-4">
												{entry.direction === 'DIRECTION_CREDIT' ? 'Credit' : 'Debit'}
											</td>
											<td
												class="py-2 font-mono {entry.direction === 'DIRECTION_CREDIT'
													? 'text-green-600 dark:text-green-400'
													: 'text-slate-900 dark:text-slate-100'}"
											>
												{entry.direction === 'DIRECTION_CREDIT' ? '+' : '-'}₹{formatMinorAsRupees(
													entry.amountMinor
												)}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</div>
			</Card>
		{/if}
	</main>
</div>
