<script lang="ts">
	import { resolve } from '$app/paths';
	import { Card } from '$lib/components/ui';
</script>

<div class="flex min-h-screen items-center justify-center px-4 py-10">
	<Card class="w-full max-w-md text-center">
		<h1 class="text-xl font-semibold text-slate-900 dark:text-slate-100">Check your email</h1>
		<p class="mt-3 text-sm text-slate-600 dark:text-slate-400">
			We've sent a verification link to the address you registered with. Click the link to verify
			your account, then log in below.
		</p>
		<a
			href={resolve('/login')}
			class="mt-6 inline-block rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
		>
			Go to login
		</a>
	</Card>
</div>
