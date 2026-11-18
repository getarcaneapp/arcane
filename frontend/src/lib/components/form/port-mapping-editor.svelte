<script module lang="ts">
	export type PortMappingRow = {
		hostIp: string;
		hostPort: string;
		containerPort: string;
		protocol: 'tcp' | 'udp';
	};
</script>

<script lang="ts">
	import { Input } from '@arcane/ui/input';

	import { m } from '#lib/paraglide/messages.js';

	import RowListEditor from './row-list-editor.svelte';

	let {
		rows = $bindable([]),
		disabled = false
	}: {
		rows?: PortMappingRow[];
		disabled?: boolean;
	} = $props();
</script>

<RowListEditor bind:rows {disabled} createRow={() => ({ hostIp: '', hostPort: '', containerPort: '', protocol: 'tcp' })}>
	{#snippet children(row)}
		<Input
			type="text"
			placeholder={m.host_ip_placeholder()}
			bind:value={row.hostIp}
			{disabled}
			mono
			class="flex-1"
			title={m.host()}
		/>
		<Input
			type="text"
			placeholder={m.notifications_signal_port_placeholder()}
			bind:value={row.hostPort}
			{disabled}
			mono
			class="flex-1"
		/>
		<span class="hidden text-muted-foreground sm:inline">→</span>
		<Input
			type="text"
			placeholder={m.container_port_placeholder()}
			bind:value={row.containerPort}
			{disabled}
			mono
			class="flex-1"
		/>
		<select bind:value={row.protocol} {disabled} class="min-w-16 rounded-md border bg-background px-3 py-2 text-sm">
			<option value="tcp">{m.protocol_tcp()}</option>
			<option value="udp">{m.protocol_udp()}</option>
		</select>
	{/snippet}
</RowListEditor>
