<script lang="ts">
	import { resolve } from '$app/paths';
</script>

<div class="flex min-h-screen flex-col items-center justify-center px-4 text-center">
	<h1 class="text-3xl font-bold tracking-tight text-slate-900 dark:text-slate-100">FinVerse</h1>
	<p class="mt-3 max-w-md text-sm text-slate-600 dark:text-slate-400">
		A cloud-native digital banking platform, built as a production-quality demonstration of senior
		backend and platform engineering.
	</p>
	<div class="mt-8 flex gap-3">
		<a
			href={resolve('/login')}
			class="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
		>
			Log in
		</a>
		<a
			href={resolve('/register')}
			class="rounded-md border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-900 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
		>
			Create account
		</a>
	</div>
</div>
