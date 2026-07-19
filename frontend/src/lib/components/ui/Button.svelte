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
			'bg-blue-600 text-white hover:bg-blue-700 focus-visible:outline-blue-600 disabled:bg-blue-300',
		secondary:
			'bg-white text-slate-900 border border-slate-300 hover:bg-slate-50 focus-visible:outline-slate-400 dark:bg-slate-800 dark:text-slate-100 dark:border-slate-700 dark:hover:bg-slate-700',
		danger:
			'bg-red-600 text-white hover:bg-red-700 focus-visible:outline-red-600 disabled:bg-red-300'
	};
</script>

<button
	{type}
	disabled={disabled || loading}
	aria-busy={loading}
	class="inline-flex items-center justify-center gap-2 rounded-md px-4 py-2 text-sm font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-70 {variantClasses[
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
