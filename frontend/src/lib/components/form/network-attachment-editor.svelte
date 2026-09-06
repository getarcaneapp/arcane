<script module lang="ts">
	export type NetworkAttachmentRow = {
		network: string;
		// comma-separated aliases as typed by the user
		aliases: string;
		ipv4Address: string;
	};
</script>

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { AddIcon, CloseIcon } from '#lib/icons/index.js';

	let {
		rows = $bindable([]),
		networks = [],
		disabled = false
	}: {
		rows?: NetworkAttachmentRow[];
		networks?: string[];
		disabled?: boolean;
	} = $props();

	const attached = $derived(new Set(rows.map((row) => row.network)));
	const networkItems = $derived(networks.map((name) => ({ value: name, label: name, disabled: attached.has(name) })));

	function addRow() {
		rows.push({ network: '', aliases: '', ipv4Address: '' });
	}

	function removeRow(index: number) {
		rows.splice(index, 1);
	}
</script>

<div class="space-y-3">
	{#each rows as row, index (index)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
			<SearchableSelect items={networkItems} bind:value={row.network} {disabled} class="min-w-40 flex-1" />
			<Input
				type="text"
				placeholder={m.containers_aliases()}
				bind:value={row.aliases}
				{disabled}
				class="flex-1 font-mono"
				title={m.aliases_note()}
			/>
			<Input type="text" placeholder={m.static_ip()} bind:value={row.ipv4Address} {disabled} class="flex-1 font-mono" />
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
		customLabel={m.common_add()}
	/>
</div>
