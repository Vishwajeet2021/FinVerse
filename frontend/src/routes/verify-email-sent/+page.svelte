<script lang="ts">
	import { resolve } from '$app/paths';
	import { Card, Logo } from '$lib/components/ui';
</script>

<div class="auth-backdrop flex min-h-screen flex-col items-center justify-center px-4 py-10">
	<a href={resolve('/')} class="mb-6"><Logo /></a>
	<Card class="w-full max-w-md text-center">
		<div
			class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-blue-100 text-blue-600 dark:bg-blue-950 dark:text-blue-400"
			aria-hidden="true"
		>
			<svg class="h-6 w-6" viewBox="0 0 20 20" fill="currentColor">
				<path d="M3 4a2 2 0 00-2 2v.01L10 12l9-5.99V6a2 2 0 00-2-2H3z" />
				<path
					d="M18 8.24 10 13.5.29 7.51A2 2 0 000 8v6a2 2 0 002 2h16a2 2 0 002-2V8a2 2 0 00-.01-.24L18 8.24z"
				/>
			</svg>
		</div>
		<h1 class="mt-4 text-xl font-semibold text-slate-900 dark:text-slate-100">Check your email</h1>
		<p class="mt-3 text-sm text-slate-600 dark:text-slate-400">
			We've sent a verification link to the address you registered with. Click the link to verify
			your account, then log in below.
		</p>
		<a
			href={resolve('/login')}
			class="mt-6 inline-block rounded-lg bg-gradient-to-b from-blue-600 to-blue-700 px-4 py-2 text-sm font-medium text-white shadow-sm shadow-blue-600/30 transition-all hover:from-blue-500 hover:to-blue-600 active:scale-[0.98]"
		>
			Go to login
		</a>
	</Card>
</div>
