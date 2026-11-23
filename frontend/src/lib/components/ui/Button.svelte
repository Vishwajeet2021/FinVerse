<script lang="ts">
	import type { Snippet } from 'svelte';

	type Variant = 'primary' | 'secondary' | 'danger';

	interface Props {
		type?: 'button' | 'submit' | 'reset';
		variant?: Variant;
		disabled?: boolean;
		loading?: boolean;
		onclick?: (event: MouseEvent) => void;
		children: Snippet;
	}

	let {
		type = 'button',
		variant = 'primary',
		disabled = false,
		loading = false,
		onclick,
		children
	}: Props = $props();

	const variantClasses: Record<Variant, string> = {
		primary:
			'bg-gradient-to-b from-blue-600 to-blue-700 text-white shadow-sm shadow-blue-600/30 hover:from-blue-500 hover:to-blue-600 focus-visible:outline-blue-600 disabled:from-blue-300 disabled:to-blue-300 disabled:shadow-none',
		secondary:
			'bg-white text-slate-900 border border-slate-300 hover:bg-slate-50 focus-visible:outline-slate-400 dark:bg-slate-800 dark:text-slate-100 dark:border-slate-700 dark:hover:bg-slate-700',
		danger:
			'bg-gradient-to-b from-red-600 to-red-700 text-white shadow-sm shadow-red-600/30 hover:from-red-500 hover:to-red-600 focus-visible:outline-red-600 disabled:from-red-300 disabled:to-red-300 disabled:shadow-none'
	};
</script>

<button
	{type}
	disabled={disabled || loading}
	aria-busy={loading}
	class="inline-flex items-center justify-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-all focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-70 disabled:active:scale-100 {variantClasses[
		variant
	]}"
	{onclick}
>
	{#if loading}
		<span
			class="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"
			aria-hidden="true"
		></span>
	{/if}
	{@render children()}
</button>
