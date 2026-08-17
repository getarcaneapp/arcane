<script module lang="ts">
	export type KeyValueRow = { key: string; value: string };
</script>

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages';
	import { AddIcon, CloseIcon } from '#lib/icons';

	let {
		rows = $bindable([]),
		keyPlaceholder = 'KEY',
		valuePlaceholder = 'value',
		addLabel,
		disabled = false
	}: {
		rows?: KeyValueRow[];
		keyPlaceholder?: string;
		valuePlaceholder?: string;
		addLabel?: string;
		disabled?: boolean;
	} = $props();

	function addRow() {
		rows.push({ key: '', value: '' });
	}

	function removeRow(index: number) {
		rows.splice(index, 1);
	}
</script>

<div class="space-y-3">
	{#each rows as row, index (index)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
			<Input type="text" placeholder={keyPlaceholder} bind:value={row.key} {disabled} class="flex-1 font-mono" />
			<span class="hidden font-mono text-muted-foreground sm:inline">=</span>
			<Input type="text" placeholder={valuePlaceholder} bind:value={row.value} {disabled} class="flex-1 font-mono" />
			<ArcaneButton
				action="base"
				tone="ghost"
				size="icon"
				onclick={() => removeRow(index)}
				{disabled}
				class="shrink-0 text-destructive hover:text-destructive"
				icon={CloseIcon}
			/>
		</div>
	{/each}
	<ArcaneButton
		action="base"
		tone="outline"
		size="sm"
		onclick={addRow}
		{disabled}
		class="w-fit"
		icon={AddIcon}
		customLabel={addLabel ?? m.common_add()}
	/>
</div>
