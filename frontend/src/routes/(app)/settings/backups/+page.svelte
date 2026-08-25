<script lang="ts">
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import settingsStore from '#lib/stores/config-store';
	import { SettingsPageLayout, type SettingsActionButton } from '#lib/layouts';
	import { AlertIcon, BackupIcon, CloudStorageIcon, LockIcon, ResetIcon } from '#lib/icons';
	import * as Alert from '#lib/components/ui/alert';
	import { CopyButton } from '#lib/components/ui/copy-button';
	import { Input } from '#lib/components/ui/input';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import { systemBackupService } from '#lib/services/system-backup-service';
	import { hasPermission } from '#lib/utils/auth';
	import { backupDestinationOptions, backupPolicyDestinationDisplay, s3DestinationOptions } from '#lib/utils/backups';
	import type { SearchPaginationSortRequest } from '#lib/types/shared';
	import type { BackupHistoryEntry, SystemBackupDestination, SystemVolumeBackupSelectionMode } from '#lib/types/system-backup';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import * as m from '#lib/paraglide/messages.js';
	import SystemBackupTable from './system-backup-table.svelte';
	import BackupPolicyCard from '#lib/components/backup-policy-card.svelte';
	import SystemBackupScheduleDialog from './system-backup-schedule-dialog.svelte';
	import SystemVolumeScopeFields from './system-volume-scope-fields.svelte';

	let { data } = $props();
	let backups = $state(untrack(() => data.backups));
	let policyCollection = $state(untrack(() => data.policyCollection));
	let systemVolumePolicyCollection = $state(untrack(() => data.systemVolumePolicyCollection));
	let systemVolumeOptions = $state(untrack(() => data.systemVolumeOptions));
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	let scheduleOpen = $state(false);
	let scheduleType = $state<'system' | 'volume'>('system');
	let editingScheduleId = $state<string | undefined>();
	let keyOpen = $state(false);
	let actionOpen = $state(false);
	let action = $state<'create' | 'restore' | 'upload' | 'delete' | 'discover'>('create');
	let selected = $state<BackupHistoryEntry | null>(null);
	let backupType = $state<'system' | 'volume'>('system');
	let backupConfiguration = $state('custom');
	let destination = $state<SystemBackupDestination>('local');
	let s3DestinationId = $state('');
	let stopContainers = $state(false);
	let selectionMode = $state<SystemVolumeBackupSelectionMode>('all');
	let volumeNames = $state<string[]>([]);
	let ignoreAnonymous = $state(true);
	let systemVolumeOptionsLoading = $state(false);
	let recoveryKey = $state('');
	let newRecoveryKey = $state('');
	let loading = $state(false);
	let generatingKey = $state(false);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const canManageRecoveryKey = $derived(hasPermission('system-backups:recovery-key'));
	const destinationOptions = $derived(backupDestinationOptions(data.destinations.length > 0));
	const s3Options = $derived(s3DestinationOptions(data.destinations));
	const backupTypeOptions = $derived([
		{ label: m.system(), value: 'system', description: m.system_backups_type_system_description() },
		{ label: m.resource_volume_cap(), value: 'volume', description: m.system_backups_type_volume_description() }
	]);
	const configurationOptions = $derived([
		{
			label: m.system_backups_custom_configuration(),
			value: 'custom',
			description: m.system_backups_custom_configuration_description()
		},
		...(backupType === 'system' ? policyCollection.policies : systemVolumePolicyCollection.policies).map((item) => ({
			label: item.schedule,
			value: item.id,
			description: backupPolicyDestinationDisplay(item)
		}))
	]);
	// Restore and discover may target backups keyed by another instance, so
	// they accept a typed (previously generated) key. Everything else only
	// ever uses this instance's stored key.
	const actionNeedsTypedKey = $derived(action === 'restore' || action === 'discover');
	const recoveryKeyPattern = /^[A-Z2-7]{6}(-[A-Z2-7]{6}){7}$/;
	const keyError = $derived(
		actionNeedsTypedKey && recoveryKey.length > 0 && !recoveryKeyPattern.test(recoveryKey.trim())
			? m.system_backups_recovery_key_required()
			: ''
	);
	const actionKeyInvalid = $derived.by(() => {
		if (action === 'create' && backupType === 'volume') return false;
		if (actionNeedsTypedKey) return !policyCollection.recoveryKeyStored && !recoveryKeyPattern.test(recoveryKey.trim());
		// Deleting is always allowed; the backend only asks for the key when
		// the run still has snapshots to forget.
		if (action === 'delete') return false;
		return !policyCollection.recoveryKeyStored;
	});
	const destinationError = $derived(
		backupConfiguration === 'custom' &&
			((action === 'create' && destination !== 'local') || action === 'upload' || action === 'discover') &&
			!s3DestinationId
			? m.volume_backup_s3_destination_required()
			: ''
	);
	const invalid = $derived(Boolean(actionKeyInvalid || keyError || destinationError));

	function openSchedule(type: 'system' | 'volume' = 'system', id?: string) {
		scheduleType = type;
		editingScheduleId = id;
		scheduleOpen = true;
	}

	// Generating persists the key in the same step; the dialog only ever shows
	// an already-saved key, with one job: get the user to store it elsewhere.
	async function openRecoveryKey() {
		newRecoveryKey = '';
		generatingKey = true;
		try {
			const generated = await systemBackupService.generateRecoveryKey();
			await systemBackupService.setRecoveryKey(generated.recoveryKey);
			newRecoveryKey = generated.recoveryKey;
			policyCollection = { ...policyCollection, recoveryKeyStored: true };
			keyOpen = true;
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_backups_recovery_key_save_failed());
		} finally {
			generatingKey = false;
		}
	}

	function openAction(next: typeof action, backup: BackupHistoryEntry | null = null) {
		action = next;
		selected = backup;
		backupType = 'system';
		backupConfiguration = 'custom';
		destination = 'local';
		stopContainers = false;
		selectionMode = 'all';
		volumeNames = [];
		ignoreAnonymous = true;
		s3DestinationId =
			backup?.s3DestinationId ||
			policyCollection.policies.find((item) => item.s3DestinationId)?.s3DestinationId ||
			data.destinations[0]?.id ||
			'';
		recoveryKey = '';
		actionOpen = true;
	}
	function dialogTitle() {
		if (action === 'restore') return m.system_backups_restore_title();
		if (action === 'upload') return m.system_backups_upload_title();
		if (action === 'delete') return m.system_backups_delete_title();
		if (action === 'discover') return m.system_backups_discover_title();
		return m.volumes_backup_create();
	}
	function dialogDescription() {
		if (action === 'restore') return m.system_backups_restore_description();
		if (action === 'delete') return m.system_backups_delete_description();
		if (action === 'discover') return m.system_backups_discover_description();
		return backupType === 'volume' ? m.system_volume_backups_description() : m.system_backups_dialog_description();
	}
	async function refresh() {
		backups = await systemBackupService.listHistory(requestOptions);
	}

	async function loadSystemVolumeOptions() {
		if (systemVolumeOptionsLoading) return;
		systemVolumeOptionsLoading = true;
		try {
			systemVolumeOptions = await systemBackupService.listSystemVolumeOptions();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_volume_backups_options_failed());
		} finally {
			systemVolumeOptionsLoading = false;
		}
	}

	function changeBackupType(value: string) {
		backupType = value as 'system' | 'volume';
		backupConfiguration = 'custom';
		destination = 'local';
		s3DestinationId = data.destinations[0]?.id ?? '';
		if (backupType === 'volume') void loadSystemVolumeOptions();
	}

	async function openVolumeBackups(backup: BackupHistoryEntry) {
		const localEnvironment = environmentStore.getLocalEnvironment();
		if (localEnvironment) await environmentStore.setEnvironment(localEnvironment);
		await goto(`/volumes/${encodeURIComponent(backup.resourceName)}?tab=backups`);
	}

	// With a stored key the S3 repositories are scanned automatically, so
	// remote snapshots just appear in the table; the manual Find S3 backups
	// flow only exists for fresh instances that must supply an old key.
	let autoDiscovered = false;
	$effect(() => {
		if (autoDiscovered || !policyCollection.recoveryKeyStored || data.destinations.length === 0) return;
		autoDiscovered = true;
		void (async () => {
			try {
				const counts = await Promise.all(data.destinations.map((item) => systemBackupService.discover(item.id, '')));
				if (counts.some((count) => count > 0)) await refresh();
			} catch (error) {
				console.warn('S3 backup discovery failed', error);
			}
		})();
	});

	async function submitAction() {
		if (loading || invalid) return;
		loading = true;
		try {
			if (action === 'create') {
				if (backupType === 'system') {
					await systemBackupService.create(
						backupConfiguration === 'custom'
							? { destination, s3DestinationId: destination === 'local' ? '' : s3DestinationId, recoveryKey }
							: { policyId: backupConfiguration, recoveryKey }
					);
					toast.success(m.system_backups_created());
				} else {
					const result = await systemBackupService.runSystemVolumeBackups(
						backupConfiguration === 'custom'
							? {
									custom: {
										destination,
										s3DestinationId: destination === 'local' ? '' : s3DestinationId,
										stopContainers,
										selectionMode,
										volumeNames,
										ignoreAnonymous
									}
								}
							: { policyId: backupConfiguration }
					);
					toast.success(
						m.system_volume_backups_run_result({
							matched: result.matched,
							succeeded: result.succeeded,
							failed: result.failed,
							skipped: result.skipped
						})
					);
				}
				await refresh();
			} else if (action === 'restore' && selected) {
				await systemBackupService.restore(selected.id, recoveryKey);
				toast.success(m.system_backups_restore_started());
			} else if (action === 'upload' && selected) {
				await systemBackupService.upload(selected.id, s3DestinationId, recoveryKey);
				toast.success(m.backups_upload_s3_success());
				await refresh();
			} else if (action === 'delete' && selected) {
				await systemBackupService.delete(selected.id, recoveryKey);
				toast.success(m.system_backups_deleted());
				await refresh();
			} else if (action === 'discover') {
				const count = await systemBackupService.discover(s3DestinationId, recoveryKey);
				toast.success(m.system_backups_discovered({ count }));
				await refresh();
			}
			actionOpen = false;
		} catch (error) {
			const fallback =
				action === 'restore'
					? m.system_backups_restore_failed()
					: action === 'delete'
						? m.system_backups_delete_failed()
						: action === 'upload'
							? m.backups_upload_s3_failed()
							: action === 'discover'
								? m.system_backups_discover_failed()
								: backupType === 'volume'
									? m.system_volume_backups_run_failed()
									: m.system_backups_create_failed();
			toast.error(error instanceof Error ? error.message : fallback);
		} finally {
			loading = false;
		}
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => [
		...(policyCollection.recoveryKeyStored && canManageRecoveryKey
			? [
					{
						id: 'reset-recovery-key',
						action: 'refresh',
						icon: ResetIcon,
						label: m.system_backups_reset_recovery_key(),
						loading: generatingKey,
						onclick: openRecoveryKey,
						disabled: isReadOnly
					} satisfies SettingsActionButton
				]
			: []),
		{
			id: 's3-destinations',
			action: 'edit',
			icon: CloudStorageIcon,
			label: m.s3_destinations_title(),
			onclick: () => goto('/settings/backups/s3')
		},
		...(!policyCollection.recoveryKeyStored
			? [
					{
						id: 'discover',
						action: 'inspect',
						label: m.system_backups_discover(),
						onclick: () => openAction('discover'),
						disabled: isReadOnly || data.destinations.length === 0
					} satisfies SettingsActionButton
				]
			: []),
		{
			id: 'create',
			action: 'create',
			label: m.common_create(),
			disabled: isReadOnly,
			options: [
				{ label: m.jobs_schedule(), onclick: () => openSchedule() },
				{ label: m.volumes_workspace_backup(), onclick: () => openAction('create') }
			]
		}
	]);
</script>

{#snippet recoveryKeySummary()}
	{#if !policyCollection.recoveryKeyStored}
		<div class="flex items-center justify-between gap-3 rounded-md border border-amber-500/40 bg-amber-500/5 px-3 py-2">
			<div class="flex min-w-0 items-center gap-2">
				<LockIcon class="size-4 shrink-0 text-amber-500" />
				<p class="text-sm">{m.system_backups_recovery_key_needed()}</p>
			</div>
			<ArcaneButton
				action="edit"
				size="sm"
				customLabel={m.system_backups_setup_recovery_key()}
				loading={generatingKey}
				onclick={openRecoveryKey}
				disabled={isReadOnly || !canManageRecoveryKey}
			/>
		</div>
	{/if}
{/snippet}

{#snippet recoveryKeyDialog()}
	<ResponsiveDialog
		bind:open={keyOpen}
		title={m.system_backups_recovery_key()}
		description={m.system_backups_recovery_key_saved()}
		contentClass={action === 'create' ? 'sm:max-w-[760px]' : 'sm:max-w-[560px]'}
	>
		{#snippet children()}
			<div class="space-y-3 py-2">
				<div class="flex items-center gap-2">
					<Input
						id="system-backup-recovery-key"
						value={newRecoveryKey}
						readonly
						spellcheck={false}
						class="font-mono tracking-wide uppercase"
					/>
					<CopyButton text={newRecoveryKey} variant="outline" tabindex={0} class="shrink-0" />
				</div>
				<Alert.Root variant="destructive">
					<AlertIcon class="size-4" />
					<Alert.Description>{m.system_backups_recovery_key_alert()}</Alert.Description>
				</Alert.Root>
			</div>
		{/snippet}
		{#snippet footer()}
			<ArcaneButton action="confirm" customLabel={m.common_done()} onclick={() => (keyOpen = false)} />
		{/snippet}
	</ResponsiveDialog>
{/snippet}

{#snippet actionDialog()}
	<ResponsiveDialog
		bind:open={actionOpen}
		title={dialogTitle()}
		description={dialogDescription()}
		contentClass={action === 'create' ? 'sm:max-w-[760px]' : 'sm:max-w-[560px]'}
	>
		{#snippet children()}
			<div class="space-y-5 py-2">
				{#if action === 'create'}
					<SelectWithLabel
						id="manual-backup-type"
						value={backupType}
						onValueChange={changeBackupType}
						label={m.backups_backup_type()}
						description={m.backups_backup_type_description()}
						options={backupTypeOptions}
					/>
					<SelectWithLabel
						id="manual-backup-configuration"
						value={backupConfiguration}
						onValueChange={(value) => (backupConfiguration = value)}
						label={m.system_backups_backup_configuration()}
						options={configurationOptions}
					/>
					{#if backupConfiguration === 'custom'}
						<SelectWithLabel
							id="manual-backup-destination"
							value={destination}
							onValueChange={(value) => (destination = value as SystemBackupDestination)}
							label={m.backups_destination_label()}
							options={destinationOptions}
						/>
						{#if backupType === 'volume'}
							<LabeledSwitch
								id="manual-volume-backup-stop-containers"
								checked={stopContainers}
								onCheckedChange={(value) => (stopContainers = value)}
								label={m.volume_backup_stop_containers()}
								description={m.volume_backup_stop_containers_description()}
							/>
						{/if}
					{/if}
				{/if}
				{#if action === 'upload' || action === 'discover' || (action === 'create' && backupConfiguration === 'custom' && destination !== 'local')}
					<SelectWithLabel
						id="manual-system-backup-s3"
						value={s3DestinationId}
						onValueChange={(value) => (s3DestinationId = value)}
						label={m.volume_backup_s3_destination_label()}
						error={destinationError || null}
						options={s3Options}
					/>
				{/if}
				{#if action === 'create' && backupType === 'volume' && backupConfiguration === 'custom'}
					<SystemVolumeScopeFields
						idPrefix="manual-volume-backup"
						{selectionMode}
						{volumeNames}
						{ignoreAnonymous}
						options={systemVolumeOptions}
						loading={systemVolumeOptionsLoading}
						onChange={(values) => {
							selectionMode = values.selectionMode ?? selectionMode;
							volumeNames = values.volumeNames ?? volumeNames;
							ignoreAnonymous = values.ignoreAnonymous ?? ignoreAnonymous;
						}}
					/>
				{/if}
				{#if actionNeedsTypedKey}
					<TextInputWithLabel
						value={recoveryKey}
						onChange={(value) => (recoveryKey = value)}
						error={keyError || null}
						label={m.system_backups_recovery_key()}
						description={policyCollection.recoveryKeyStored
							? m.system_backups_recovery_key_saved_description()
							: m.system_backups_recovery_key_enter_description()}
						type="password"
						autocomplete="current-password"
					/>
				{:else if action !== 'delete' && !(action === 'create' && backupType === 'volume') && !policyCollection.recoveryKeyStored}
					<div class="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
						<p class="text-sm text-muted-foreground">{m.system_backups_recovery_key_needed()}</p>
						<ArcaneButton
							action="edit"
							size="sm"
							customLabel={m.system_backups_setup_recovery_key()}
							loading={generatingKey}
							onclick={() => {
								actionOpen = false;
								openRecoveryKey();
							}}
						/>
					</div>
				{/if}
			</div>
		{/snippet}
		{#snippet footer()}
			<ArcaneButton action="cancel" onclick={() => (actionOpen = false)} disabled={loading} />
			<ArcaneButton
				action={action === 'delete' ? 'remove' : action === 'restore' ? 'confirm' : action === 'create' ? 'create' : 'save'}
				customLabel={dialogTitle()}
				onclick={submitAction}
				{loading}
				disabled={loading || invalid}
			/>
		{/snippet}
	</ResponsiveDialog>
{/snippet}

<SettingsPageLayout
	title={m.system_backups_title()}
	description={m.system_backups_description()}
	icon={BackupIcon}
	pageType="management"
	showReadOnlyTag={isReadOnly}
	{actionButtons}
>
	{#snippet mainContent()}
		<div class="space-y-4">
			{@render recoveryKeySummary()}

			<div class="space-y-2">
				<h2 class="text-lg font-semibold">{m.system_backups_schedules()}</h2>
				{#if policyCollection.policies.length || systemVolumePolicyCollection.policies.length}
					<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground sm:grid-cols-2 xl:grid-cols-3">
						{#each policyCollection.policies as policy (policy.id)}
							<BackupPolicyCard
								{policy}
								resourceType="system"
								onEdit={() => openSchedule('system', policy.id)}
								editDisabled={isReadOnly}
							/>
						{/each}
						{#each systemVolumePolicyCollection.policies as policy (policy.id)}
							<BackupPolicyCard
								{policy}
								resourceType="volume"
								showStopContainers
								onEdit={() => openSchedule('volume', policy.id)}
								editDisabled={isReadOnly}
							/>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">{m.system_backups_no_schedules()}</p>
				{/if}
			</div>

			<SystemBackupTable
				bind:backups
				bind:requestOptions
				onChanged={(options) => systemBackupService.listHistory(options)}
				onRestore={(item) => openAction('restore', item)}
				onUpload={(item) => openAction('upload', item)}
				onDelete={(item) => openAction('delete', item)}
				onOpenVolume={openVolumeBackups}
			/>
		</div>
	{/snippet}
	{#snippet additionalContent()}
		{#if scheduleOpen}
			<SystemBackupScheduleDialog
				bind:open={scheduleOpen}
				initialType={scheduleType}
				policyId={editingScheduleId}
				systemPolicies={policyCollection.policies}
				volumePolicies={systemVolumePolicyCollection.policies}
				recoveryKeyStored={policyCollection.recoveryKeyStored}
				destinations={data.destinations}
				onSystemSaved={(policies) => (policyCollection = { ...policyCollection, policies })}
				onVolumeSaved={(policies) => (systemVolumePolicyCollection = { policies })}
			/>
		{/if}

		{@render recoveryKeyDialog()}
		{@render actionDialog()}
	{/snippet}
</SettingsPageLayout>
