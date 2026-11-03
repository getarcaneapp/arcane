<script lang="ts">
	let {
		label,
		options,
		current,
		onSelect
	}: {
		label: string;
		options: { value: string; label: string }[];
		current: string;
		onSelect: (value: string) => void;
	} = $props();
</script>

<div class="flex shrink-0 gap-0.5 rounded-xl bg-muted p-0.5 text-xs whitespace-nowrap" role="group" aria-label={label}>
	{#each options as option (option.value)}
		<button
			type="button"
			class="rounded-lg px-2 py-1 transition-colors {current === option.value
				? 'bg-primary/15 font-medium text-primary dark:text-primary-tint'
				: 'text-muted-foreground hover:text-primary dark:hover:text-primary-tint'}"
			aria-pressed={current === option.value}
			onclick={() => onSelect(option.value)}
		>
			{option.label}
		</button>
	{/each}
</div>
