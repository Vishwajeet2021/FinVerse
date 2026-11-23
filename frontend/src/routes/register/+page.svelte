<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { register } from '$lib/api/auth';
	import { Alert, Button, Card, Input, Logo } from '$lib/components/ui';
	import { describeError } from '$lib/utils/errors';
	import { isPasswordLongEnough, isValidEmail, passwordsMatch } from '$lib/utils/validation';

	let email = $state('');
	let phone = $state('');
	let password = $state('');
	let confirmPassword = $state('');
	let submitting = $state(false);
	let formError = $state<string | null>(null);

	let emailError = $state<string | null>(null);
	let passwordError = $state<string | null>(null);
	let confirmPasswordError = $state<string | null>(null);

	function validate(): boolean {
		emailError = isValidEmail(email) ? null : 'Enter a valid email address.';
		passwordError = isPasswordLongEnough(password)
			? null
			: 'Password must be at least 12 characters.';
		confirmPasswordError = passwordsMatch(password, confirmPassword)
			? null
			: 'Passwords do not match.';
		return !emailError && !passwordError && !confirmPasswordError;
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		if (!validate()) return;

		submitting = true;
		try {
			await register({ email, phone: phone.trim() ? phone.trim() : undefined, password });
			// We route to an informational "check your email" page rather than logging the user
			// in immediately: the account is created with emailVerified: false, and letting an
			// unverified session straight into the app would undercut FR-AUTH-2's verification
			// flow. Documented as a deliberate choice per the task spec, not an oversight.
			await goto(resolve('/verify-email-sent'));
		} catch (err) {
			formError = describeError(err);
		} finally {
			submitting = false;
		}
	}
</script>

<div class="auth-backdrop flex min-h-screen flex-col items-center justify-center px-4 py-10">
	<a href={resolve('/')} class="mb-6"><Logo /></a>
	<Card class="w-full max-w-md">
		<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">Create your account</h1>
		<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">Takes less than a minute.</p>

		<form class="mt-6 flex flex-col gap-4" onsubmit={handleSubmit}>
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
				error={emailError}
			/>
			<Input id="phone" label="Phone (optional)" type="tel" autocomplete="tel" bind:value={phone} />
			<Input
				id="password"
				label="Password"
				type="password"
				autocomplete="new-password"
				required
				bind:value={password}
				error={passwordError}
			/>
			<Input
				id="confirm-password"
				label="Confirm password"
				type="password"
				autocomplete="new-password"
				required
				bind:value={confirmPassword}
				error={confirmPasswordError}
			/>

			<Button type="submit" loading={submitting}>Create account</Button>
		</form>

		<p class="mt-6 text-center text-sm text-slate-600 dark:text-slate-400">
			Already have an account?
			<a
				href={resolve('/login')}
				class="font-medium text-blue-600 hover:underline dark:text-blue-400">Log in</a
			>
		</p>
	</Card>
</div>
