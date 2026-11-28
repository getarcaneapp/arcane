<script lang="ts" module>
	import type { GlobalProjectVolume } from '#lib/types/project.js';

	export function projectVolumeValue(volume: GlobalProjectVolume): string {
		return JSON.stringify([volume.projectId, volume.key]);
	}

	export function parseProjectVolumeValue(value: string): GlobalProjectVolume {
		const [projectId, key] = JSON.parse(value) as [string, string];
		return { projectId, key };
	}
</script>

<script lang="ts">
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Checkbox from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { ScrollArea } from '#lib/components/ui/scroll-area/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { LoadingSpinnerIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import type { SystemBackupOption, SystemVolumeBackupOption, SystemVolumeSelectionMode } from '#lib/types/backup.js';
	import type { GlobalProjectBackupOption } from '#lib/types/project.js';

	let {
		idPrefix,
		kind,
		selectionMode,
		selected,
		ignoreAnonymous = false,
		options,
		loading = false,
		onChange
	}: {
		idPrefix: string;
		kind: 'system' | 'project' | 'volume' | 'projectVolume';
		selectionMode: SystemVolumeSelectionMode;
		selected: string[];
		ignoreAnonymous?: boolean;
		options: SystemBackupOption[] | SystemVolumeBackupOption[] | GlobalProjectBackupOption[];
		loading?: boolean;
		onChange: (values: { selectionMode?: SystemVolumeSelectionMode; selected?: string[]; ignoreAnonymous?: boolean }) => void;
	} = $props();

	let search = $state('');
	const volumeText = {
		mode: m.system_volume_backups_selection_mode(),
		modeDescription: m.system_volume_backups_selection_description(),
		all: m.system_volume_backups_selection_all(),
		allDescription: m.system_volume_backups_selection_all_description(),
		allowlistDescription: m.system_volume_backups_selection_allowlist_description(),
		blocklistDescription: m.system_volume_backups_selection_blocklist_description(),
		included: m.system_volume_backups_included_volumes(),
		excluded: m.system_volume_backups_excluded_volumes(),
		allowlistHelp: m.system_volume_backups_allowlist_help(),
		blocklistHelp: m.system_volume_backups_blocklist_help(),
		search: m.system_volume_backups_search_volumes(),
		empty: m.system_volume_backups_no_matching_volumes()
	};
	const text = $derived.by(() => {
		switch (kind) {
			case 'system':
				return {
					mode: m.system_selection(),
					modeDescription: m.rules_checked_against_systems(),
					all: m.all_systems(),
					allDescription: m.all_systems_description(),
					allowlistDescription: m.include_only_selected_systems(),
					blocklistDescription: m.include_all_except_selected_systems(),
					included: m.included_systems(),
					excluded: m.excluded_systems(),
					allowlistHelp: m.checked_systems_included(),
					blocklistHelp: m.checked_systems_excluded(),
					search: m.search_systems(),
					empty: m.no_matching_systems()
				};
			case 'project':
				return {
					mode: m.project_selection(),
					modeDescription: m.rules_checked_against_projects(),
					all: m.all_projects(),
					allDescription: m.all_projects_description(),
					allowlistDescription: m.include_only_selected_projects(),
					blocklistDescription: m.include_all_except_selected_projects(),
					included: m.included_projects(),
					excluded: m.excluded_projects(),
					allowlistHelp: m.checked_projects_included(),
					blocklistHelp: m.checked_projects_excluded(),
					search: m.search_projects(),
					empty: m.no_matching_projects()
				};
			case 'projectVolume':
				return {
					...volumeText,
					modeDescription: m.rules_checked_against_project_volumes(),
					allDescription: m.all_project_volumes_description()
				};
			default:
				return volumeText;
		}
	});
	const selectionOptions = $derived([
		{ label: text.all, value: 'all', description: text.allDescription },
		{ label: m.system_volume_backups_selection_allowlist(), value: 'allowlist', description: text.allowlistDescription },
		{ label: m.system_volume_backups_selection_blocklist(), value: 'blocklist', description: text.blocklistDescription }
	]);
	type ScopeItem = {
		value: string;
		label: string;
		detail?: string;
		group?: string;
		anonymous: boolean;
		missing: boolean;
		available: boolean;
	};

	const items = $derived.by((): ScopeItem[] => {
		if (kind === 'projectVolume') {
			const saved = selected.map(parseProjectVolumeValue);
			return (options as GlobalProjectBackupOption[]).flatMap((project) => {
				const group = project.environmentName ? `${project.environmentName} · ${project.name}` : project.name;
				return [
					...project.volumes.map((volume) => ({
						value: projectVolumeValue({ projectId: project.id, key: volume.key }),
						label: volume.key,
						detail: volume.name === volume.key ? undefined : volume.name,
						group,
						anonymous: false,
						missing: volume.missing ?? false,
						available: true
					})),
					...saved
						.filter((volume) => volume.projectId === project.id && !project.volumes.some((live) => live.key === volume.key))
						.map((volume) => ({
							value: projectVolumeValue(volume),
							label: volume.key,
							group,
							anonymous: false,
							missing: false,
							available: false
						}))
				];
			});
		}
		return options.map((option) => {
			if ('id' in option) {
				return {
					value: option.id,
					label: option.name,
					group: 'environmentName' in option ? option.environmentName : undefined,
					anonymous: false,
					missing: false,
					available: option.available
				};
			}
			return { value: option.name, label: option.name, anonymous: option.anonymous, missing: false, available: option.available };
		});
	});
	const filteredOptions = $derived.by(() => {
		const term = search.trim().toLowerCase();
		if (!term) return items;
		return items.filter((option) =>
			[option.label, option.detail, option.group].some((value) => value?.toLowerCase().includes(term))
		);
	});

	function toggle(value: string, checked: boolean) {
		if (checked) {
			onChange({ selected: Array.from(new Set([...selected, value])).sort() });
			return;
		}
		onChange({ selected: selected.filter((candidate) => candidate !== value) });
	}
</script>

<SettingsSection class="pt-2">
	<SettingsRow for={`${idPrefix}-selection-mode`} label={text.mode} description={text.modeDescription}>
		{#snippet children({ describedBy })}
			<SelectWithLabel
				id={`${idPrefix}-selection-mode`}
				hideLabel
				value={selectionMode}
				onValueChange={(value) => onChange({ selectionMode: value as SystemVolumeSelectionMode })}
				label={text.mode}
				options={selectionOptions}
				aria-describedby={describedBy}
			/>
		{/snippet}
	</SettingsRow>

	{#if selectionMode !== 'all'}
		<SettingsRow
			label={selectionMode === 'allowlist' ? text.included : text.excluded}
			description={selectionMode === 'allowlist' ? text.allowlistHelp : text.blocklistHelp}
			layout="wide"
		>
			{#snippet children({ describedBy })}
				<div class="space-y-2">
					<Input bind:value={search} placeholder={text.search} aria-describedby={describedBy} />
					<div class="h-56 overflow-hidden rounded-md border">
						<ScrollArea class="h-full">
							{#if loading}
								<div class="flex h-24 items-center justify-center"><LoadingSpinnerIcon class="size-5 animate-spin" /></div>
							{:else}
								<div class="divide-y divide-border/50">
									{#each filteredOptions as option, index (option.value)}
										{#if option.group && option.group !== filteredOptions[index - 1]?.group}
											<p class="bg-muted/30 px-3 py-1.5 text-xs font-medium text-muted-foreground">{option.group}</p>
										{/if}
										<label class="flex cursor-pointer items-center gap-3 px-3 py-2.5 hover:bg-muted/40">
											<Checkbox.Root
												checked={selected.includes(option.value)}
												onCheckedChange={(value) => toggle(option.value, value === true)}
												aria-describedby={describedBy}
											/>
											<span class="min-w-0 flex-1 truncate text-sm">
												{option.label}
												{#if option.detail}<span class="text-muted-foreground"> · {option.detail}</span>{/if}
											</span>
											{#if option.missing}<Badge variant="gray" size="sm">{m.compose_service_not_created()}</Badge>{/if}
											{#if option.anonymous}<Badge variant="amber" size="sm">{m.system_volume_backups_anonymous()}</Badge>{/if}
											{#if !option.available}<Badge variant="gray" size="sm">{m.system_volume_backups_unavailable()}</Badge>{/if}
										</label>
									{/each}
									{#if filteredOptions.length === 0}
										<p class="px-3 py-8 text-center text-sm text-muted-foreground">{text.empty}</p>
									{/if}
								</div>
							{/if}
						</ScrollArea>
					</div>
				</div>
			{/snippet}
		</SettingsRow>
	{/if}

	{#if kind === 'volume'}
		<SettingsRow
			for={`${idPrefix}-ignore-anonymous`}
			label={m.system_volume_backups_ignore_anonymous()}
			description={m.system_volume_backups_ignore_anonymous_description()}
			layout="switch"
		>
			{#snippet children({ describedBy })}
				<Switch
					id={`${idPrefix}-ignore-anonymous`}
					checked={ignoreAnonymous}
					onCheckedChange={(value) => onChange({ ignoreAnonymous: value })}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	{/if}
</SettingsSection>
