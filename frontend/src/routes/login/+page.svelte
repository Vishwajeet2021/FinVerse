<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { Alert, Button, Card, Input } from '$lib/components/ui';
	import { loginAndSetSession } from '$lib/stores/auth';
	import { describeError } from '$lib/utils/errors';

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);
	let formError = $state<string | null>(null);

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		formError = null;
		submitting = true;
		try {
			await loginAndSetSession(email, password);
			await goto(resolve('/dashboard'));
		} catch (err) {
			formError = describeError(err);
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center px-4 py-10">
	<Card class="w-full max-w-md">
		<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">Log in</h1>

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
	</Card>
</div>
