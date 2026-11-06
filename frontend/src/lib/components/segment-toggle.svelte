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

<div
	class="flex shrink-0 gap-0.5 rounded-xl bg-muted p-0.5 text-xs-plus leading-5 whitespace-nowrap"
	role="group"
	aria-label={label}
>
	{#each options as option (option.value)}
		<button
			type="button"
			class="inline-flex items-center justify-center rounded-xl px-2 py-1 transition-colors pointer-coarse:min-h-touch-target pointer-coarse:min-w-touch-target {current ===
			option.value
				? 'bg-primary/15 font-medium text-primary-text'
				: 'text-muted-foreground hover:text-primary-text'}"
			aria-pressed={current === option.value}
			onclick={() => onSelect(option.value)}
		>
			{option.label}
		</button>
	{/each}
</div>
