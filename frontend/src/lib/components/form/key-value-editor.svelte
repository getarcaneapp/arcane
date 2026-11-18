<script module lang="ts">
	export type KeyValueRow = { key: string; value: string };
</script>

<script lang="ts">
	import { Input } from '@arcane/ui/input';

	import { m } from '#lib/paraglide/messages.js';

	import RowListEditor from './row-list-editor.svelte';

	let {
		rows = $bindable([]),
		keyPlaceholder = m.key_placeholder(),
		valuePlaceholder = m.value_placeholder(),
		addLabel,
		disabled = false
	}: {
		rows?: KeyValueRow[];
		keyPlaceholder?: string;
		valuePlaceholder?: string;
		addLabel?: string;
		disabled?: boolean;
	} = $props();
</script>

<RowListEditor bind:rows {disabled} createRow={() => ({ key: '', value: '' })} addLabel={addLabel ?? m.common_add()}>
	{#snippet children(row)}
		<Input type="text" placeholder={keyPlaceholder} bind:value={row.key} {disabled} mono class="flex-1" />
		<span class="hidden font-mono text-muted-foreground sm:inline">=</span>
		<Input type="text" placeholder={valuePlaceholder} bind:value={row.value} {disabled} mono class="flex-1" />
	{/snippet}
</RowListEditor>
