<script lang="ts">
	type Autocomplete = 'email' | 'tel' | 'new-password' | 'current-password' | 'off';

	interface Props {
		id: string;
		label: string;
		type?: 'text' | 'email' | 'tel' | 'password';
		value: string;
		placeholder?: string;
		required?: boolean;
		autocomplete?: Autocomplete;
		error?: string | null;
	}

	let {
		id,
		label,
		type = 'text',
		value = $bindable(),
		placeholder = '',
		required = false,
		autocomplete,
		error = null
	}: Props = $props();
</script>

<div class="flex flex-col gap-1">
	<label for={id} class="text-sm font-medium text-slate-700 dark:text-slate-200">{label}</label>
	<input
		{id}
		{type}
		{placeholder}
		{required}
		{autocomplete}
		bind:value
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={error ? `${id}-error` : undefined}
		class="rounded-lg border px-3 py-2 text-sm shadow-sm transition-shadow focus:ring-2 focus:ring-blue-500/60 focus:outline-none dark:bg-slate-800 dark:text-slate-100 {error
			? 'border-red-500'
			: 'border-slate-300 dark:border-slate-700'}"
	/>
	{#if error}
		<p id="{id}-error" class="text-sm text-red-600 dark:text-red-400">{error}</p>
	{/if}
</div>
