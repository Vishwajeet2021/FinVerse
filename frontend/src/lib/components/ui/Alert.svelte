<script lang="ts">
	type Variant = 'error' | 'success' | 'info';

	interface Props {
		variant?: Variant;
		message: string;
	}

	let { variant = 'info', message }: Props = $props();

	const variantClasses: Record<Variant, string> = {
		error:
			'bg-red-50 text-red-800 border-red-200 dark:bg-red-950 dark:text-red-200 dark:border-red-900',
		success:
			'bg-green-50 text-green-800 border-green-200 dark:bg-green-950 dark:text-green-200 dark:border-green-900',
		info: 'bg-blue-50 text-blue-800 border-blue-200 dark:bg-blue-950 dark:text-blue-200 dark:border-blue-900'
	};
</script>

<div role="alert" class="rounded-md border px-4 py-3 text-sm {variantClasses[variant]}">
	{message}
</div>
