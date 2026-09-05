<script module lang="ts">
	export type VolumeMountRow = {
		kind: 'bind' | 'volume' | 'mount';
		source: string;
		target: string;
		readOnly: boolean;
		// raw bind options beyond ro/rw (e.g. z, cached) preserved verbatim
		rawOptions?: string;
		// original mount type for kind === 'mount' rows (bind/volume/tmpfs/...)
		mountType?: string;
	};
</script>

<script lang="ts">
	import { useId } from 'bits-ui';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { m } from '#lib/paraglide/messages';
	import { AddIcon, CloseIcon } from '#lib/icons';

	let {
		rows = $bindable([]),
		volumes = [],
		disabled = false
	}: {
		rows?: VolumeMountRow[];
		volumes?: string[];
		disabled?: boolean;
	} = $props();

	const volumeItems = $derived(volumes.map((name) => ({ value: name, label: name })));

	const id = useId();

	function addRow(kind: 'bind' | 'volume') {
		rows.push({ kind, source: '', target: '', readOnly: false });
	}

	function removeRow(index: number) {
		rows.splice(index, 1);
	}
</script>

<div class="space-y-3">
	<p class="text-xs text-muted-foreground">{m.volume_mapping_help()}</p>
	{#each rows as row, index (index)}
		<div class="space-y-2 rounded-lg border border-border/50 p-3">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-end sm:gap-3">
				<Badge variant="outline" class="w-fit shrink-0 font-mono text-xs">
					{row.kind === 'mount' ? (row.mountType ?? 'mount') : row.kind}
				</Badge>
				<div class="flex-1 space-y-2">
					<Label for={`${id}-source-${index}`}
						>{row.kind === 'volume' || row.mountType === 'volume'
							? m.common_name()
							: row.mountType && row.mountType !== 'bind'
								? m.containers_mount_label_source()
								: m.host_path()}</Label
					>
					{#if row.kind === 'volume'}
						<SearchableSelect
							triggerId={`${id}-source-${index}`}
							items={volumeItems}
							bind:value={row.source}
							{disabled}
							class="flex-1"
						/>
					{:else}
						<Input
							type="text"
							id={`${id}-source-${index}`}
							placeholder={m.container_source_path_or_volume()}
							bind:value={row.source}
							disabled={disabled || row.kind === 'mount'}
							class="flex-1 font-mono"
						/>
					{/if}
				</div>
				<div class="flex-1 space-y-2">
					<Label for={`${id}-target-${index}`}>{m.container_path()}</Label>
					<Input
						type="text"
						id={`${id}-target-${index}`}
						placeholder={m.container_path()}
						bind:value={row.target}
						disabled={disabled || row.kind === 'mount'}
						class="flex-1 font-mono"
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
			<div class="flex items-center gap-4">
				<div class="flex items-center space-x-2">
					<Checkbox id={`${id}-readonly-${index}`} bind:checked={row.readOnly} {disabled} />
					<Label for={`${id}-readonly-${index}`} class="text-sm font-normal">{m.read_only_label()}</Label>
				</div>
				{#if row.rawOptions}
					<Badge variant="secondary" class="font-mono text-xs" title={m.mount_options_note()}>
						{row.rawOptions}
					</Badge>
				{/if}
			</div>
		</div>
	{/each}
	<div class="flex flex-wrap gap-2">
		<ArcaneButton
			action="base"
			tone="outline"
			size="sm"
			onclick={() => addRow('bind')}
			{disabled}
			class="w-fit"
			icon={AddIcon}
			customLabel={m.add_bind_mount_button()}
		/>
		<ArcaneButton
			action="base"
			tone="outline"
			size="sm"
			onclick={() => addRow('volume')}
			{disabled}
			class="w-fit"
			icon={AddIcon}
			customLabel={m.add_volume_mount_button()}
		/>
	</div>
</div>
