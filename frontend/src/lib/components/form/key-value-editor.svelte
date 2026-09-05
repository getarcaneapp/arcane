<script module lang="ts">
	export type KeyValueRow = { key: string; value: string };
</script>

<script lang="ts">
	import { useId } from 'bits-ui';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
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

	const id = useId();

	function addRow() {
		rows.push({ key: '', value: '' });
	}

	function removeRow(index: number) {
		rows.splice(index, 1);
	}
</script>

<div class="space-y-3">
	{#each rows as row, index (index)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-end sm:gap-3">
			<div class="flex-1 space-y-2">
				<Label for={`${id}-key-${index}`}>{m.key()}</Label><Input
					id={`${id}-key-${index}`}
					type="text"
					placeholder={keyPlaceholder}
					bind:value={row.key}
					{disabled}
					class="font-mono"
				/>
			</div>
			<span class="hidden font-mono text-muted-foreground sm:inline">=</span>
			<div class="flex-1 space-y-2">
				<Label for={`${id}-value-${index}`}>{m.value()}</Label><Input
					id={`${id}-value-${index}`}
					type="text"
					placeholder={valuePlaceholder}
					bind:value={row.value}
					{disabled}
					class="font-mono"
				/>
			</div>
			<ArcaneButton
				action="base"
				tone="ghost"
				size="icon"
				onclick={() => removeRow(index)}
				aria-label={m.common_remove()}
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
