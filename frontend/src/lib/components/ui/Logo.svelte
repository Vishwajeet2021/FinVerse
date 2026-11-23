<script lang="ts">
	interface Props {
		size?: 'sm' | 'md' | 'lg';
		withWordmark?: boolean;
	}

	let { size = 'md', withWordmark = true }: Props = $props();

	const markSizes: Record<string, string> = {
		sm: 'h-7 w-7 text-sm',
		md: 'h-9 w-9 text-base',
		lg: 'h-12 w-12 text-xl'
	};
	const wordmarkSizes: Record<string, string> = {
		sm: 'text-base',
		md: 'text-lg',
		lg: 'text-2xl'
	};
</script>

<div class="flex items-center gap-2.5">
	<div
		class="flex shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 via-blue-600 to-cyan-500 font-bold text-white shadow-sm shadow-blue-600/30 {markSizes[
			size
		]}"
		aria-hidden="true"
	>
		F
	</div>
	{#if withWordmark}
		<span class="font-semibold tracking-tight text-slate-900 dark:text-white {wordmarkSizes[size]}">
			FinVerse
		</span>
	{/if}
</div>
