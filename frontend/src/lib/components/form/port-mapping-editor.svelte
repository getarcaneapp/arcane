<script module lang="ts">
	export type PortMappingRow = {
		hostIp: string;
		hostPort: string;
		containerPort: string;
		protocol: 'tcp' | 'udp';
	};
</script>

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
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

	function addRow() {
		rows.push({ hostIp: '', hostPort: '', containerPort: '', protocol: 'tcp' });
	}

	function removeRow(index: number) {
		rows.splice(index, 1);
	}
</script>

<div class="space-y-3">
	{#each rows as row, index (index)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
			<Input type="text" placeholder="0.0.0.0" bind:value={row.hostIp} {disabled} class="flex-1 font-mono" title={m.host()} />
			<Input type="text" placeholder="8080" bind:value={row.hostPort} {disabled} class="flex-1 font-mono" />
			<span class="hidden text-muted-foreground sm:inline">→</span>
			<Input type="text" placeholder="80" bind:value={row.containerPort} {disabled} class="flex-1 font-mono" />
			<select bind:value={row.protocol} {disabled} class="min-w-16 rounded-md border bg-background px-3 py-2 text-sm">
				<option value="tcp">TCP</option>
				<option value="udp">UDP</option>
			</select>
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
