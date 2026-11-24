<script lang="ts">
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ProjectVolume } from '#lib/types/project.js';

	// Notes on the volumes whose data the copy leaves out, worded for an export or a backup.
	type VolumeNotes = {
		backupStore: string;
		missing: string;
		driverOpts: string;
		external: string;
	};

	let {
		volumes,
		overrides = $bindable({}),
		selectable,
		notes,
		idPrefix,
		disabled = false
	}: {
		volumes: ProjectVolume[];
		// The user's choices by volume key; a volume without one is checked when its data is included.
		overrides?: Record<string, boolean>;
		selectable: (volume: ProjectVolume) => boolean;
		notes: VolumeNotes;
		idPrefix: string;
		disabled?: boolean;
	} = $props();

	function volumeNote(volume: ProjectVolume): string | null {
		if (volume.backupStore) return notes.backupStore;
		if (volume.missing) return notes.missing;
		if (volume.kind === 'driverOpts') return notes.driverOpts;
		if (volume.kind === 'external' && !volume.included) return notes.external;
		if (volume.inUse) return m.volume_in_use_elsewhere();
		return null;
	}
</script>

<ul class="space-y-2">
	{#each volumes as volume (volume.key)}
		{@const note = volumeNote(volume)}
		<li class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
			<Checkbox
				id={`${idPrefix}-${volume.key}`}
				class="mt-0.5"
				checked={selectable(volume) && (overrides[volume.key] ?? volume.included)}
				disabled={!selectable(volume) || disabled}
				onCheckedChange={(checked) => (overrides = { ...overrides, [volume.key]: checked === true })}
			/>
			<div class="grid min-w-0 gap-1">
				<Label for={`${idPrefix}-${volume.key}`} class="mb-0 break-all">{volume.key}</Label>
				<span class="font-mono text-xs break-all text-muted-foreground">{volume.name}</span>
				{#if note}
					<span class={['text-xs', volume.inUse ? 'text-warning' : 'text-muted-foreground']}>{note}</span>
				{/if}
			</div>
		</li>
	{/each}
</ul>
