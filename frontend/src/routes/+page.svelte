<script lang="ts">
	import { resolve } from '$app/paths';
	import { Logo } from '$lib/components/ui';
</script>

<div class="auth-backdrop flex min-h-screen flex-col items-center justify-center px-4 text-center">
	<Logo size="lg" withWordmark={false} />
	<h1
		class="mt-5 bg-gradient-to-r from-slate-900 to-slate-600 bg-clip-text text-4xl font-bold tracking-tight text-transparent dark:from-white dark:to-slate-400"
	>
		FinVerse
	</h1>
	<p class="mt-3 max-w-md text-sm text-slate-600 dark:text-slate-400">
		A cloud-native digital banking platform, built as a production-quality demonstration of senior
		backend and platform engineering.
	</p>
	<div class="mt-8 flex gap-3">
		<a
			href={resolve('/login')}
			class="rounded-lg bg-gradient-to-b from-blue-600 to-blue-700 px-4 py-2 text-sm font-medium text-white shadow-sm shadow-blue-600/30 transition-all hover:from-blue-500 hover:to-blue-600 active:scale-[0.98]"
		>
			Log in
		</a>
		<a
			href={resolve('/register')}
			class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-900 transition-all hover:bg-slate-50 active:scale-[0.98] dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
		>
			Create account
		</a>
	</div>
</div>
