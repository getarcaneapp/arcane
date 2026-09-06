<script lang="ts">
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { ScrollArea } from '#lib/components/ui/scroll-area/index.js';
	import * as Checkbox from '#lib/components/ui/checkbox/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { LoadingSpinnerIcon } from '#lib/icons/index.js';
	import type { SystemVolumeBackupOption, SystemVolumeBackupSelectionMode } from '#lib/types/system-backup.js';
	import * as m from '#lib/paraglide/messages.js';

	let {
		idPrefix,
		selectionMode,
		volumeNames,
		ignoreAnonymous,
		options,
		loading = false,
		onChange
	}: {
		idPrefix: string;
		selectionMode: SystemVolumeBackupSelectionMode;
		volumeNames: string[];
		ignoreAnonymous: boolean;
		options: SystemVolumeBackupOption[];
		loading?: boolean;
		onChange: (values: {
			selectionMode?: SystemVolumeBackupSelectionMode;
			volumeNames?: string[];
			ignoreAnonymous?: boolean;
		}) => void;
	} = $props();

	let search = $state('');
	const selectionOptions = $derived([
		{
			label: m.system_volume_backups_selection_all(),
			value: 'all',
			description: m.system_volume_backups_selection_all_description()
		},
		{
			label: m.system_volume_backups_selection_allowlist(),
			value: 'allowlist',
			description: m.system_volume_backups_selection_allowlist_description()
		},
		{
			label: m.system_volume_backups_selection_blocklist(),
			value: 'blocklist',
			description: m.system_volume_backups_selection_blocklist_description()
		}
	]);
	const filteredOptions = $derived.by(() => {
		const term = search.trim().toLowerCase();
		return term ? options.filter((option) => option.name.toLowerCase().includes(term)) : options;
	});

	function toggleVolume(name: string, checked: boolean) {
		onChange({
			volumeNames: checked
				? Array.from(new Set([...volumeNames, name])).sort()
				: volumeNames.filter((candidate) => candidate !== name)
		});
	}
</script>

<div class="space-y-5 border-t pt-5">
	<SelectWithLabel
		id={`${idPrefix}-selection-mode`}
		value={selectionMode}
		onValueChange={(value) => onChange({ selectionMode: value as SystemVolumeBackupSelectionMode })}
		label={m.system_volume_backups_selection_mode()}
		description={m.system_volume_backups_selection_description()}
		options={selectionOptions}
	/>

	{#if selectionMode !== 'all'}
		<div class="space-y-2">
			<div>
				<Label>
					{selectionMode === 'allowlist'
						? m.system_volume_backups_included_volumes()
						: m.system_volume_backups_excluded_volumes()}
				</Label>
				<p class="mt-0.5 text-xs text-muted-foreground">
					{selectionMode === 'allowlist' ? m.system_volume_backups_allowlist_help() : m.system_volume_backups_blocklist_help()}
				</p>
			</div>
			<Input bind:value={search} placeholder={m.system_volume_backups_search_volumes()} />
			<ScrollArea class="h-56 rounded-md border">
				{#if loading}
					<div class="flex h-24 items-center justify-center"><LoadingSpinnerIcon class="size-5 animate-spin" /></div>
				{:else}
					<div class="divide-y divide-border/50">
						{#each filteredOptions as option (option.name)}
							<label class="flex cursor-pointer items-center gap-3 px-3 py-2.5 hover:bg-muted/40">
								<Checkbox.Root
									checked={volumeNames.includes(option.name)}
									onCheckedChange={(value) => toggleVolume(option.name, value === true)}
								/>
								<span class="min-w-0 flex-1 truncate text-sm">{option.name}</span>
								{#if option.anonymous}<Badge variant="amber" size="sm">{m.system_volume_backups_anonymous()}</Badge>{/if}
								{#if !option.available}<Badge variant="gray" size="sm">{m.system_volume_backups_unavailable()}</Badge>{/if}
							</label>
						{/each}
						{#if filteredOptions.length === 0}
							<p class="px-3 py-8 text-center text-sm text-muted-foreground">
								{m.system_volume_backups_no_matching_volumes()}
							</p>
						{/if}
					</div>
				{/if}
			</ScrollArea>
		</div>
	{/if}

	<LabeledSwitch
		id={`${idPrefix}-ignore-anonymous`}
		checked={ignoreAnonymous}
		onCheckedChange={(value) => onChange({ ignoreAnonymous: value })}
		label={m.system_volume_backups_ignore_anonymous()}
		description={m.system_volume_backups_ignore_anonymous_description()}
	/>
</div>
