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

<div class="flex shrink-0 gap-0.5 text-xs whitespace-nowrap" role="group" aria-label={label}>
	{#each options as option (option.value)}
		<button
			type="button"
			class="rounded-md px-2 py-1 transition-colors {current === option.value
				? 'bg-muted font-medium text-foreground'
				: 'text-muted-foreground hover:text-foreground'}"
			aria-pressed={current === option.value}
			onclick={() => onSelect(option.value)}
		>
			{option.label}
		</button>
	{/each}
</div>
