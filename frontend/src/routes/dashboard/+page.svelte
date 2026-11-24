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
	import { createMyProfile, getMyProfile } from '$lib/api/customers';
	import { getPayment, initiatePayment, type Payment, type Rail } from '$lib/api/payments';
	import { RAIL_OPTIONS, describePaymentFailure } from '$lib/utils/payments';
	import { ApiError } from '$lib/api/http';
	import { Alert, Button, Card, Logo } from '$lib/components/ui';
	import { logoutAndClearSession } from '$lib/stores/auth';
	import { describeError } from '$lib/utils/errors';
	import { MAX_ACCOUNT_NUMBER_LENGTH, isAdultDob, isValidPan } from '$lib/utils/validation';
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
		if (!accounts || accounts.length === 0) return;
		if (selectedAccountId && accounts.some((a) => a.id === selectedAccountId)) return;
		selectedAccountId = (accounts.find((a) => a.status === 'ACTIVE') ?? accounts[0]).id;
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

	let toAccountNumber = $state('');
	let transferAmount = $state('');
	let transferring = $state(false);
	let transferError = $state<string | null>(null);
	let transferSuccess = $state<string | null>(null);
	let transferInfo = $state<string | null>(null);
	let rail = $state<Rail>('INTERNAL');
	const railHint = $derived(RAIL_OPTIONS.find((r) => r.value === rail)?.hint ?? '');

	// Asynchronous rails answer PROCESSING first and clear a few seconds later; poll until the
	// payment reaches a terminal state (or give up after a minute and say it is still pending).
	async function waitForSettlement(initial: Payment): Promise<Payment> {
		let payment = initial;
		for (let i = 0; i < 60 && payment.status === 'PROCESSING'; i++) {
			await new Promise((r) => setTimeout(r, 1000));
			payment = await getPayment(payment.id);
		}
		return payment;
	}

	async function handleTransfer(event: SubmitEvent) {
		event.preventDefault();
		transferError = null;
		transferSuccess = null;
		transferInfo = null;

		if (!selectedAccountId) return;
		const amountMinor = rupeesToMinor(transferAmount);
		if (amountMinor === null) {
			transferError = 'Enter a valid amount greater than zero.';
			return;
		}
		if (!toAccountNumber.trim()) {
			transferError = 'Enter a destination account number.';
			return;
		}

		transferring = true;
		try {
			let payment = await initiatePayment(
				selectedAccountId,
				toAccountNumber.trim(),
				amountMinor,
				rail
			);
			if (payment.status === 'PROCESSING') {
				transferInfo = `${payment.rail} payment accepted and clearing…`;
				payment = await waitForSettlement(payment);
				transferInfo = null;
			}
			if (payment.status === 'COMPLETED') {
				transferSuccess = payment.transactionId
					? `Transfer completed (transaction ${payment.transactionId}).`
					: 'Transfer completed.';
				toAccountNumber = '';
				transferAmount = '';
			} else if (payment.status === 'PROCESSING') {
				transferInfo = 'Still clearing. Check your transaction history in a moment.';
			} else {
				transferError = describePaymentFailure(payment.failureReason);
			}
			// The ledger moves before the balance projection catches up, so refresh both.
			await queryClient.invalidateQueries({ queryKey: ['balance', selectedAccountId] });
			await queryClient.invalidateQueries({ queryKey: ['statement', selectedAccountId] });
		} catch (err) {
			transferError = describeError(err, "Couldn't complete the transfer. Please try again.");
		} finally {
			transferring = false;
		}
	}

	let profileName = $state('');
	let profileDob = $state('');
	let profilePan = $state('');
	let creatingProfile = $state(false);
	let profileError = $state<string | null>(null);
	async function handleCreateProfile(event: SubmitEvent) {
		event.preventDefault();
		profileError = null;
		if (!profileName.trim() || !profileDob || !profilePan.trim()) {
			profileError = 'Please fill in your name, date of birth and PAN.';
			return;
		}
		if (!isValidPan(profilePan.trim().toUpperCase())) {
			profileError = 'PAN must be in the format ABCDE1234F.';
			return;
		}
		if (!isAdultDob(profileDob)) {
			profileError = 'You must be at least 18 years old to open an account.';
			return;
		}
		creatingProfile = true;
		try {
			await createMyProfile({
				fullName: profileName.trim(),
				dob: profileDob,
				pan: profilePan.trim().toUpperCase()
			});
			await queryClient.invalidateQueries({ queryKey: ['customer', 'me'] });
		} catch (err) {
			profileError = describeError(err, "Couldn't create your profile. Please try again.");
		} finally {
			creatingProfile = false;
		}
	}

	function isProfileMissing(err: unknown): boolean {
		return err instanceof ApiError && err.status === 404;
	}

	const statusPillClasses: Record<string, string> = {
		ACTIVE: 'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400',
		FROZEN: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400',
		CLOSED: 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400'
	};

	const kycPillClasses: Record<string, string> = {
		APPROVED: 'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400',
		PENDING: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400',
		UNDER_REVIEW: 'bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-400',
		REJECTED: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-400'
	};
</script>

{#snippet sectionIcon(path: string)}
	<span
		class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-400"
	>
		<svg class="h-4.5 w-4.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
			<path d={path} />
		</svg>
	</span>
{/snippet}

<div class="min-h-screen">
	<header
		class="sticky top-0 z-10 flex items-center justify-between border-b border-slate-200/80 bg-white/80 px-6 py-3.5 backdrop-blur-sm dark:border-slate-800 dark:bg-slate-900/80"
	>
		<Logo size="sm" />
		<div class="flex items-center gap-2">
			<Button variant="secondary" onclick={() => goto(resolve('/scheduled'))}>
				Scheduled &amp; recurring
			</Button>
			<Button variant="secondary" onclick={handleLogout} loading={loggingOut}>Log out</Button>
		</div>
	</header>

	<main class="mx-auto max-w-3xl space-y-6 px-4 py-10">
		<Card>
			<div class="flex items-center gap-3">
				{@render sectionIcon('M10 9a3 3 0 100-6 3 3 0 000 6zm-7 9a7 7 0 1114 0H3z')}
				<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Your profile</h2>
			</div>
			<div class="mt-4">
				{#if profileQuery.isPending}
					<div class="h-4 w-1/3 animate-pulse rounded bg-slate-200 dark:bg-slate-800"></div>
				{:else if profileQuery.isError}
					{#if isProfileMissing(profileQuery.error)}
						<p class="text-sm text-slate-600 dark:text-slate-400">
							We don't have a customer profile for your account yet. Tell us a little about you.
						</p>
						<form class="mt-3 grid gap-3 sm:grid-cols-3" onsubmit={handleCreateProfile}>
							<input
								aria-label="Full name"
								placeholder="Full name"
								maxlength="120"
								bind:value={profileName}
								class="rounded-lg border border-slate-300 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
							/>
							<input
								aria-label="Date of birth"
								type="date"
								bind:value={profileDob}
								class="rounded-lg border border-slate-300 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
							/>
							<input
								aria-label="PAN"
								placeholder="PAN"
								maxlength="10"
								bind:value={profilePan}
								class="rounded-lg border border-slate-300 px-3 py-2 text-sm uppercase dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
							/>
							<div class="sm:col-span-3">
								<Button type="submit" loading={creatingProfile}>Create profile</Button>
							</div>
						</form>
						{#if profileError}
							<div class="mt-3"><Alert variant="error" message={profileError} /></div>
						{/if}
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
							<dt
								class="text-xs font-medium tracking-wide text-slate-500 uppercase dark:text-slate-400"
							>
								Name
							</dt>
							<dd class="mt-1 text-base font-medium text-slate-900 dark:text-slate-100">
								{profileQuery.data.fullName}
							</dd>
						</div>
						<div>
							<dt
								class="text-xs font-medium tracking-wide text-slate-500 uppercase dark:text-slate-400"
							>
								KYC tier
							</dt>
							<dd class="mt-1 text-base font-medium text-slate-900 dark:text-slate-100">
								{profileQuery.data.kycTier}
							</dd>
						</div>
						<div>
							<dt
								class="text-xs font-medium tracking-wide text-slate-500 uppercase dark:text-slate-400"
							>
								KYC status
							</dt>
							<dd class="mt-1">
								<span
									class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium {kycPillClasses[
										profileQuery.data.kycStatus
									] ?? 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'}"
								>
									{profileQuery.data.kycStatus}
								</span>
							</dd>
						</div>
					</dl>
				{/if}
			</div>
		</Card>

		<Card>
			<div class="flex flex-wrap items-center justify-between gap-3">
				<div class="flex items-center gap-3">
					{@render sectionIcon(
						'M2.5 5A1.5 1.5 0 014 3.5h12A1.5 1.5 0 0117.5 5v1h-15V5zm0 3h15v6.5A1.5 1.5 0 0116 16H4a1.5 1.5 0 01-1.5-1.5V8zm10.5 3a1 1 0 100 2h2a1 1 0 100-2h-2z'
					)}
					<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">Your accounts</h2>
				</div>
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
							{@const isClosed = account.status === 'CLOSED'}
							<li>
								<button
									type="button"
									disabled={isClosed}
									onclick={() => (selectedAccountId = account.id)}
									class="flex w-full items-center gap-3 rounded-lg px-2 py-3 text-left transition-colors {isClosed
										? 'cursor-not-allowed opacity-60'
										: 'hover:bg-slate-50 dark:hover:bg-slate-800'} {selectedAccountId === account.id
										? 'bg-blue-50/60 ring-1 ring-blue-200 ring-inset dark:bg-blue-950/40 dark:ring-blue-900'
										: ''}"
								>
									<span
										class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-xs font-semibold {account.productType ===
										'SAVINGS'
											? 'bg-cyan-100 text-cyan-700 dark:bg-cyan-950 dark:text-cyan-400'
											: 'bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-400'}"
										aria-hidden="true"
									>
										{account.productType === 'SAVINGS' ? 'SA' : 'CU'}
									</span>
									<span class="min-w-0 flex-1">
										<span class="flex items-center gap-2">
											<span class="text-sm font-medium text-slate-900 dark:text-slate-100">
												{account.productType} · {account.accountNumber}
											</span>
											<span
												class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium {statusPillClasses[
													account.status
												] ?? 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400'}"
											>
												{account.status}
											</span>
										</span>
									</span>
									<span class="font-mono text-sm font-medium text-slate-700 dark:text-slate-300">
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
				<div class="flex items-center gap-3">
					{@render sectionIcon(
						'M15 5.25a.75.75 0 01.75.75v.75h.5a.75.75 0 010 1.5h-.5v.75a.75.75 0 01-1.5 0v-.75h-4a.75.75 0 010-1.5h4V6a.75.75 0 01.75-.75zM5 12a.75.75 0 01.75.75v.75h9.5a.75.75 0 010 1.5h-9.5v.75a.75.75 0 01-1.5 0v-.75h-.5a.75.75 0 010-1.5h.5v-.75A.75.75 0 015 12z'
					)}
					<div>
						<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
							Transfer money
						</h2>
						<p class="text-xs text-slate-500 dark:text-slate-400">
							From account {selectedAccountId}
						</p>
					</div>
				</div>

				<form class="mt-4 flex flex-col gap-4 sm:flex-row sm:items-end" onsubmit={handleTransfer}>
					<div class="flex flex-1 flex-col gap-1">
						<label
							for="toAccountNumber"
							class="text-sm font-medium text-slate-700 dark:text-slate-200"
						>
							To account number
						</label>
						<input
							id="toAccountNumber"
							type="text"
							bind:value={toAccountNumber}
							placeholder="destination account number"
							maxlength={MAX_ACCOUNT_NUMBER_LENGTH}
							class="rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm transition-shadow focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
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
							class="rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm transition-shadow focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
						/>
					</div>
					<div class="flex w-44 flex-col gap-1">
						<label for="rail" class="text-sm font-medium text-slate-700 dark:text-slate-200">
							Payment method
						</label>
						<select
							id="rail"
							bind:value={rail}
							class="rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
						>
							{#each RAIL_OPTIONS as option (option.value)}
								<option value={option.value}>{option.label}</option>
							{/each}
						</select>
					</div>
					<Button type="submit" loading={transferring}>Send</Button>
				</form>

				<p class="mt-2 text-xs text-slate-500 dark:text-slate-400">{railHint}</p>

				{#if transferInfo}
					<div class="mt-3"><Alert variant="info" message={transferInfo} /></div>
				{/if}
				{#if transferError}
					<div class="mt-3"><Alert variant="error" message={transferError} /></div>
				{/if}
				{#if transferSuccess}
					<div class="mt-3"><Alert variant="success" message={transferSuccess} /></div>
				{/if}
			</Card>

			<Card>
				<div class="flex items-center gap-3">
					{@render sectionIcon(
						'M2 4.75A.75.75 0 012.75 4h14.5a.75.75 0 010 1.5H2.75A.75.75 0 012 4.75zM2 10a.75.75 0 01.75-.75h14.5a.75.75 0 010 1.5H2.75A.75.75 0 012 10zm0 5.25a.75.75 0 01.75-.75h14.5a.75.75 0 010 1.5H2.75a.75.75 0 01-.75-.75z'
					)}
					<h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">
						Transaction history
					</h2>
				</div>
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
						<ul class="divide-y divide-slate-100 dark:divide-slate-800">
							{#each statementQuery.data as entry (entry.legId)}
								{@const isCredit = entry.direction === 'DIRECTION_CREDIT'}
								<li
									class="flex items-center gap-3 py-2.5 transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/60"
								>
									<span
										class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full {isCredit
											? 'bg-green-100 text-green-600 dark:bg-green-950 dark:text-green-400'
											: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'}"
										aria-hidden="true"
									>
										<svg class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
											{#if isCredit}
												<path
													fill-rule="evenodd"
													d="M10 18a.75.75 0 01-.75-.75V4.66L5.03 8.9a.75.75 0 01-1.06-1.06l5.5-5.5a.75.75 0 011.06 0l5.5 5.5a.75.75 0 01-1.06 1.06l-4.22-4.24v12.59A.75.75 0 0110 18z"
													clip-rule="evenodd"
												/>
											{:else}
												<path
													fill-rule="evenodd"
													d="M10 2a.75.75 0 01.75.75v12.59l4.22-4.24a.75.75 0 111.06 1.06l-5.5 5.5a.75.75 0 01-1.06 0l-5.5-5.5a.75.75 0 111.06-1.06l4.22 4.24V2.75A.75.75 0 0110 2z"
													clip-rule="evenodd"
												/>
											{/if}
										</svg>
									</span>
									<span class="min-w-0 flex-1">
										<span
											class="block truncate font-mono text-xs text-slate-500 dark:text-slate-400"
											title={entry.transactionId}
										>
											{entry.transactionId}
										</span>
										<span class="block text-xs text-slate-400 dark:text-slate-500">
											{isCredit ? 'Credit' : 'Debit'}
										</span>
									</span>
									<span
										class="font-mono text-sm font-medium {isCredit
											? 'text-green-600 dark:text-green-400'
											: 'text-slate-900 dark:text-slate-100'}"
									>
										{isCredit ? '+' : '-'}₹{formatMinorAsRupees(entry.amountMinor)}
									</span>
								</li>
							{/each}
						</ul>
					{/if}
				</div>
			</Card>
		{/if}
	</main>
</div>
