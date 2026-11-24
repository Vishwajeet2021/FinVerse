<script lang="ts">
	import QRCode from 'qrcode';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { ApiError } from '$lib/api/http';
	import { Alert, Button, Card, Input, Logo } from '$lib/components/ui';
	import {
		beginEnrollment,
		completeEnrollment,
		completeMfaLogin,
		loginAndSetSession
	} from '$lib/stores/auth';
	import { describeError } from '$lib/utils/errors';

	// password -> (code | enroll -> recovery) -> dashboard. Every step after 'password' exists only
	// because the server answered the password check with a challenge instead of a session.
	type Step = 'password' | 'code' | 'enroll' | 'recovery';

	let step = $state<Step>('password');
	let email = $state('');
	let password = $state('');
	let code = $state('');
	// Held in memory for the length of this sign-in only. It proves the password step passed and
	// opens nothing else.
	let challengeToken = $state('');
	let enrollment = $state<{ secret: string; qr: string } | null>(null);
	let recoveryCodes = $state<string[]>([]);
	let saved = $state(false);
	let copied = $state(false);

	let submitting = $state(false);
	let formError = $state<string | null>(null);

	function backToPassword(message: string | null = null) {
		step = 'password';
		password = '';
		code = '';
		challengeToken = '';
		enrollment = null;
		formError = message;
	}

	// A challenge the server no longer honours (expired, used up, account locked) cannot be
	// retried with another code, so those send the user back to the start with an explanation.
	function isDeadChallenge(err: unknown): boolean {
		return (
			err instanceof ApiError &&
			(err.code === 'MFA_CHALLENGE_INVALID' || err.code === 'ACCOUNT_LOCKED')
		);
	}

	async function handlePassword(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		submitting = true;
		try {
			const outcome = await loginAndSetSession(email, password);
			if (outcome.kind === 'session') {
				await goto(resolve('/dashboard'));
				return;
			}
			challengeToken = outcome.challengeToken;
			password = '';
			if (outcome.kind === 'mfa') {
				step = 'code';
				return;
			}
			const started = await beginEnrollment(outcome.challengeToken);
			enrollment = {
				secret: started.secret,
				qr: await QRCode.toDataURL(started.otpauthUri, {
					margin: 1,
					width: 192,
					errorCorrectionLevel: 'M'
				})
			};
			step = 'enroll';
		} catch (err) {
			formError = describeError(err);
		} finally {
			submitting = false;
		}
	}

	async function handleCode(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		submitting = true;
		try {
			await completeMfaLogin(email, challengeToken, code.trim());
			await goto(resolve('/dashboard'));
		} catch (err) {
			if (isDeadChallenge(err)) {
				backToPassword(describeError(err));
			} else {
				formError = describeError(err);
				code = '';
			}
		} finally {
			submitting = false;
		}
	}

	async function handleEnroll(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		submitting = true;
		try {
			recoveryCodes = await completeEnrollment(email, challengeToken, code.trim());
			challengeToken = '';
			enrollment = null;
			code = '';
			step = 'recovery';
		} catch (err) {
			if (isDeadChallenge(err)) {
				backToPassword(describeError(err));
			} else {
				formError = describeError(err);
				code = '';
			}
		} finally {
			submitting = false;
		}
	}

	async function copyCodes() {
		try {
			await navigator.clipboard.writeText(recoveryCodes.join('\n'));
			copied = true;
		} catch {
			copied = false;
			formError = 'Could not copy automatically. Please write the codes down instead.';
		}
	}

	async function finish() {
		recoveryCodes = [];
		await goto(resolve('/dashboard'));
	}

	// Groups of four are easier to type into an authenticator app by hand.
	const grouped = (secret: string) => secret.match(/.{1,4}/g)?.join(' ') ?? secret;
</script>

<div class="auth-backdrop flex min-h-screen flex-col items-center justify-center px-4 py-10">
	<a href={resolve('/')} class="mb-6"><Logo /></a>
	<Card class="w-full max-w-md">
		{#if step === 'password'}
			<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">Log in</h1>
			<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
				Welcome back — enter your details.
			</p>

			<form class="mt-6 flex flex-col gap-4" onsubmit={handlePassword}>
				{#if formError}
					<Alert variant="error" message={formError} />
				{/if}

				<Input
					id="email"
					label="Email"
					type="email"
					autocomplete="email"
					required
					bind:value={email}
				/>
				<Input
					id="password"
					label="Password"
					type="password"
					autocomplete="current-password"
					required
					bind:value={password}
				/>

				<Button type="submit" loading={submitting}>Log in</Button>
			</form>

			<p class="mt-6 text-center text-sm text-slate-600 dark:text-slate-400">
				Don't have an account?
				<a
					href={resolve('/register')}
					class="font-medium text-blue-600 hover:underline dark:text-blue-400"
				>
					Create one
				</a>
			</p>
		{:else if step === 'code'}
			<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">
				Two-step verification
			</h1>
			<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
				Enter the 6-digit code from your authenticator app. If you have lost your phone, use one of
				your recovery codes instead.
			</p>

			<form class="mt-6 flex flex-col gap-4" onsubmit={handleCode}>
				{#if formError}
					<Alert variant="error" message={formError} />
				{/if}
				<Input
					id="code"
					label="Code"
					autocomplete="one-time-code"
					inputmode="text"
					maxlength={16}
					required
					bind:value={code}
				/>
				<Button type="submit" loading={submitting}>Verify</Button>
				<Button variant="secondary" onclick={() => backToPassword()}>Back</Button>
			</form>
		{:else if step === 'enroll' && enrollment}
			<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">
				Set up two-step verification
			</h1>
			<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
				Your role requires it. Scan the code with an authenticator app (Google Authenticator,
				1Password, Authy…), then enter the 6-digit code it shows.
			</p>

			<div class="mt-5 flex justify-center">
				<img
					src={enrollment.qr}
					alt="QR code for your authenticator app"
					width="192"
					height="192"
					class="rounded-lg border border-slate-200 bg-white p-1 dark:border-slate-700"
				/>
			</div>
			<p class="mt-3 text-center text-xs text-slate-500 dark:text-slate-400">
				Can't scan? Enter this key by hand:
			</p>
			<p
				class="mt-1 text-center font-mono text-sm tracking-wider break-all text-slate-800 dark:text-slate-200"
			>
				{grouped(enrollment.secret)}
			</p>

			<form class="mt-6 flex flex-col gap-4" onsubmit={handleEnroll}>
				{#if formError}
					<Alert variant="error" message={formError} />
				{/if}
				<Input
					id="code"
					label="6-digit code"
					autocomplete="one-time-code"
					inputmode="numeric"
					maxlength={6}
					required
					bind:value={code}
				/>
				<Button type="submit" loading={submitting}>Turn on and continue</Button>
				<Button variant="secondary" onclick={() => backToPassword()}>Cancel</Button>
			</form>
		{:else if step === 'recovery'}
			<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">
				Save your recovery codes
			</h1>
			<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
				If you lose your phone, each of these signs you in once. They are shown only now. Keep them
				somewhere safe and separate from your password.
			</p>

			<ul
				class="mt-5 grid grid-cols-2 gap-2 rounded-lg bg-slate-100 p-4 font-mono text-sm text-slate-800 dark:bg-slate-800 dark:text-slate-100"
			>
				{#each recoveryCodes as recoveryCode (recoveryCode)}
					<li>{recoveryCode}</li>
				{/each}
			</ul>

			<div class="mt-4 flex flex-col gap-4">
				{#if formError}
					<Alert variant="error" message={formError} />
				{/if}
				<Button variant="secondary" onclick={copyCodes}>{copied ? 'Copied' : 'Copy codes'}</Button>
				<label class="flex items-start gap-2 text-sm text-slate-700 dark:text-slate-300">
					<input type="checkbox" bind:checked={saved} class="mt-0.5" />
					I have saved these recovery codes.
				</label>
				<Button disabled={!saved} onclick={finish}>Continue to dashboard</Button>
			</div>
		{/if}
	</Card>
</div>
