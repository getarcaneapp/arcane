<script module lang="ts">
	export type PortMappingRow = {
		hostIp: string;
		hostPort: string;
		containerPort: string;
		protocol: 'tcp' | 'udp';
	};
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
		disabled = false
	}: {
		rows?: PortMappingRow[];
		disabled?: boolean;
	} = $props();

	const id = useId();

	function addRow() {
		rows.push({ hostIp: '', hostPort: '', containerPort: '', protocol: 'tcp' });
	}

	function removeRow(index: number) {
		rows.splice(index, 1);
	}
</script>

{#snippet portField(
	row: PortMappingRow,
	index: number,
	field: 'hostIp' | 'hostPort' | 'containerPort',
	label: string,
	placeholder: string
)}
	<div class="flex-1 space-y-2">
		<Label for={`${id}-${field}-${index}`}>{label}</Label>
		<Input id={`${id}-${field}-${index}`} type="text" {placeholder} bind:value={row[field]} {disabled} class="font-mono" />
	</div>
{/snippet}

<div class="space-y-3">
	<p class="text-xs text-muted-foreground">{m.port_mapping_help()}</p>
	{#each rows as row, index (index)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-end sm:gap-3">
			{@render portField(row, index, 'hostIp', m.ports_host_ip(), '0.0.0.0')}
			{@render portField(row, index, 'hostPort', m.ports_host_port(), '8080')}
			{@render portField(row, index, 'containerPort', m.ports_container_port(), '80')}
			<div class="space-y-2">
				<Label for={`${id}-protocol-${index}`}>{m.protocol()}</Label>
				<select
					id={`${id}-protocol-${index}`}
					bind:value={row.protocol}
					{disabled}
					class="h-9 min-w-16 rounded-md border bg-background px-3 text-sm"
				>
					<option value="tcp">TCP</option>
					<option value="udp">UDP</option>
				</select>
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
		customLabel={m.common_add()}
	/>
</div>
