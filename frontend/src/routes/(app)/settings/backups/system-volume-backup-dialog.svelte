<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import BackupPolicyFields from '#lib/components/backup-policy-fields.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Input } from '#lib/components/ui/input';
	import { ScrollArea } from '#lib/components/ui/scroll-area';
	import * as Checkbox from '#lib/components/ui/checkbox';
	import { Label } from '#lib/components/ui/label';
	import { Badge } from '#lib/components/ui/badge';
	import type { BackupPolicyForm } from '#lib/types/backup';
	import type { S3Destination } from '#lib/types/s3-destination';
	import type {
		SystemVolumeBackupConfig,
		SystemVolumeBackupOption,
		SystemVolumeBackupSelectionMode
	} from '#lib/types/system-backup';
	import { backupDestinationFromFlags, backupPolicyDestinationValues } from '#lib/utils/backups';
	import * as m from '#lib/paraglide/messages.js';

	let {
		open = $bindable(),
		config,
		options,
		destinations,
		updateConfig,
		onSaved
	}: {
		open: boolean;
		config: SystemVolumeBackupConfig;
		options: SystemVolumeBackupOption[];
		destinations: S3Destination[];
		updateConfig: (config: SystemVolumeBackupConfig) => Promise<SystemVolumeBackupConfig>;
		onSaved: (config: SystemVolumeBackupConfig) => void;
	} = $props();

	let form = $state<BackupPolicyForm>({
		enabled: false,
		schedule: '0 0 2 * * *',
		retentionCount: 7,
		stopContainers: false,
		destination: 'local',
		s3DestinationId: ''
	});
	let selectionMode = $state<SystemVolumeBackupSelectionMode>('all');
	let volumeNames = $state<string[]>([]);
	let ignoreAnonymous = $state(true);
	let search = $state('');
	let saving = $state(false);
	let serverError = $state('');

	const scheduleError = $derived(
		!form.schedule.trim()
			? m.jobs_cron_required()
			: form.schedule.trim().split(/\s+/).length !== 6
				? m.jobs_cron_invalid()
				: serverError
	);
	const retentionError = $derived(
		Number.isInteger(Number(form.retentionCount)) && form.retentionCount >= 0 && form.retentionCount <= 3650
			? null
			: m.volume_backup_retention_invalid()
	);
	const destinationError = $derived(
		form.destination !== 'local' && !form.s3DestinationId ? m.volume_backup_s3_destination_required() : null
	);
	const invalid = $derived(Boolean(scheduleError || retentionError || destinationError));
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

	$effect(() => {
		if (!open) return;
		form = {
			enabled: config.enabled,
			schedule: config.schedule,
			retentionCount: config.retentionCount,
			stopContainers: config.stopContainers,
			destination: backupDestinationFromFlags(config.localEnabled, config.s3Enabled),
			s3DestinationId: config.s3DestinationId ?? ''
		};
		selectionMode = config.selectionMode;
		volumeNames = [...config.volumeNames];
		ignoreAnonymous = config.ignoreAnonymous;
		search = '';
		serverError = '';
	});

	function updateForm(values: Partial<BackupPolicyForm>) {
		form = { ...form, ...values };
		serverError = '';
	}

	function toggleVolume(name: string, checked: boolean) {
		volumeNames = checked
			? Array.from(new Set([...volumeNames, name])).sort()
			: volumeNames.filter((candidate) => candidate !== name);
	}

	async function save() {
		if (saving || invalid) return;
		saving = true;
		serverError = '';
		try {
			const destination = backupPolicyDestinationValues(form.destination, form.s3DestinationId);
			const saved = await updateConfig({
				...config,
				enabled: form.enabled,
				schedule: form.schedule,
				retentionCount: Number(form.retentionCount),
				stopContainers: form.stopContainers ?? false,
				...destination,
				selectionMode,
				volumeNames,
				ignoreAnonymous
			});
			onSaved(saved);
			open = false;
			toast.success(m.system_volume_backups_saved());
		} catch (error) {
			const message = error instanceof Error ? error.message : m.system_volume_backups_save_failed();
			if (/cron|schedule/i.test(message)) serverError = message;
			else toast.error(message);
		} finally {
			saving = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={m.system_volume_backups_title()}
	description={m.system_volume_backups_description()}
	contentClass="sm:max-w-[760px]"
>
	{#snippet children()}
		<div class="space-y-6 py-2">
			<BackupPolicyFields
				idPrefix="system-volume-backup"
				{form}
				{destinations}
				{scheduleError}
				{retentionError}
				{destinationError}
				enabledDescription={m.system_volume_backups_enabled_description()}
				schedulePlaceholder="0 0 2 * * *"
				showStopContainers
				onChange={updateForm}
			/>

			<div class="border-t pt-5">
				<SelectWithLabel
					id="system-volume-backup-selection-mode"
					value={selectionMode}
					onValueChange={(value) => (selectionMode = value as SystemVolumeBackupSelectionMode)}
					label={m.system_volume_backups_selection_mode()}
					description={m.system_volume_backups_selection_description()}
					options={selectionOptions}
				/>
			</div>

			{#if selectionMode !== 'all'}
				<div class="space-y-2">
					<div>
						<Label
							>{selectionMode === 'allowlist'
								? m.system_volume_backups_included_volumes()
								: m.system_volume_backups_excluded_volumes()}</Label
						>
						<p class="mt-0.5 text-xs text-muted-foreground">
							{selectionMode === 'allowlist'
								? m.system_volume_backups_allowlist_help()
								: m.system_volume_backups_blocklist_help()}
						</p>
					</div>
					<Input bind:value={search} placeholder={m.system_volume_backups_search_volumes()} />
					<ScrollArea class="h-56 rounded-md border">
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
								<p class="px-3 py-8 text-center text-sm text-muted-foreground">{m.system_volume_backups_no_matching_volumes()}</p>
							{/if}
						</div>
					</ScrollArea>
				</div>
			{/if}

			<LabeledSwitch
				id="system-volume-backup-ignore-anonymous"
				checked={ignoreAnonymous}
				onCheckedChange={(value) => (ignoreAnonymous = value)}
				label={m.system_volume_backups_ignore_anonymous()}
				description={m.system_volume_backups_ignore_anonymous_description()}
			/>
		</div>
	{/snippet}
	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (open = false)} disabled={saving} />
		<ArcaneButton action="save" onclick={save} loading={saving} disabled={saving || invalid} />
	{/snippet}
</ResponsiveDialog>
