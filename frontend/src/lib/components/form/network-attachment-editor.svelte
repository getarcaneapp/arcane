<script module lang="ts">
	export type NetworkAttachmentRow = {
		network: string;
		aliases: string;
		ipv4Address: string;
	};
</script>

<script lang="ts">
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import RowListEditor from './row-list-editor.svelte';

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
</script>

<RowListEditor bind:rows {disabled} createRow={() => ({ network: '', aliases: '', ipv4Address: '' })}>
	{#snippet children(row)}
		<SearchableSelect items={networkItems} bind:value={row.network} {disabled} class="min-w-40 flex-1" />
		<Input
			type="text"
			placeholder={m.containers_aliases()}
			bind:value={row.aliases}
			{disabled}
			mono
			class="flex-1"
			title={m.aliases_note()}
		/>
		<Input type="text" placeholder={m.static_ip()} bind:value={row.ipv4Address} {disabled} mono class="flex-1" />
	{/snippet}
</RowListEditor>
