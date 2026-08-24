<script lang="ts">
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import settingsStore from '#lib/stores/config-store';
	import { SettingsPageLayout, type SettingsActionButton } from '#lib/layouts';
	import { AlertIcon, BackupIcon, CloudStorageIcon, InfoIcon, LockIcon, ResetIcon } from '#lib/icons';
	import * as Alert from '#lib/components/ui/alert';
	import { CopyButton } from '#lib/components/ui/copy-button';
	import { Input } from '#lib/components/ui/input';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import { systemBackupService } from '#lib/services/system-backup-service';
	import { hasPermission } from '#lib/utils/auth';
	import { backupDestinationOptions, backupPolicyDestinationDisplay, s3DestinationOptions } from '#lib/utils/backups';
	import type { SearchPaginationSortRequest } from '#lib/types/shared';
	import type { SystemBackupDestination, SystemBackupRun } from '#lib/types/system-backup';
	import * as m from '#lib/paraglide/messages.js';
	import SystemBackupTable from './system-backup-table.svelte';
	import BackupPolicyDialog from '#lib/components/backup-policy-dialog.svelte';
	import BackupPolicyCard from '#lib/components/backup-policy-card.svelte';
	import BackupFilePicker from '#lib/components/backup-file-picker.svelte';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';
	import type { BackupFileProvider } from '#lib/types/backup';
	import { queryKeys } from '#lib/query/query-keys';
	import { useQueryClient } from '@tanstack/svelte-query';

	let { data } = $props();
	const queryClient = useQueryClient();
	let backups = $state(untrack(() => data.backups));
	let policyCollection = $state(untrack(() => data.policyCollection));
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	let policyOpen = $state(false);
	let editingPolicyId = $state<string | undefined>();
	let keyOpen = $state(false);
	let actionOpen = $state(false);
	let action = $state<'create' | 'restore' | 'upload' | 'delete' | 'discover'>('create');
	let selected = $state<SystemBackupRun | null>(null);
	let backupConfiguration = $state('custom');
	let destination = $state<SystemBackupDestination>('local');
	let s3DestinationId = $state('');
	let recoveryKey = $state('');
	let newRecoveryKey = $state('');
	let loading = $state(false);
	let generatingKey = $state(false);
	let restoreFilesOpen = $state(false);
	let restoreFilesTarget = $state<SystemBackupRun | null>(null);
	let restoreFilesRecoveryKey = $state('');
	let restoreFilesProvider = $state<BackupFileProvider | null>(null);
	let restoreFilesSelectedPaths = $state<string[]>([]);
	let restoreFilesSelectAll = $state(false);
	let restoreFilesSearch = $state('');
	let restoreFilesLoaded = $state(false);
	let restoringFiles = $state(false);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const canManageRecoveryKey = $derived(hasPermission('system-backups:recovery-key'));
	const destinationOptions = $derived(backupDestinationOptions(data.destinations.length > 0));
	const s3Options = $derived(s3DestinationOptions(data.destinations));
	const configurationOptions = $derived([
		{
			label: m.system_backups_custom_configuration(),
			value: 'custom',
			description: m.system_backups_custom_configuration_description()
		},
		...policyCollection.policies.map((item) => ({
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
	const restoreFilesKeyError = $derived(
		restoreFilesRecoveryKey.length > 0 && !recoveryKeyPattern.test(restoreFilesRecoveryKey.trim())
			? m.system_backups_recovery_key_required()
			: ''
	);
	const restoreFilesKeyInvalid = $derived(
		Boolean(restoreFilesKeyError) ||
			(!policyCollection.recoveryKeyStored && !recoveryKeyPattern.test(restoreFilesRecoveryKey.trim()))
	);
	const keyError = $derived(
		actionNeedsTypedKey && recoveryKey.length > 0 && !recoveryKeyPattern.test(recoveryKey.trim())
			? m.system_backups_recovery_key_required()
			: ''
	);
	const actionKeyInvalid = $derived.by(() => {
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

	function openPolicy(id?: string) {
		editingPolicyId = id;
		policyOpen = true;
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

	function openAction(next: typeof action, backup: SystemBackupRun | null = null) {
		action = next;
		selected = backup;
		backupConfiguration = 'custom';
		destination = 'local';
		s3DestinationId =
			backup?.s3DestinationId ||
			policyCollection.policies.find((item) => item.s3DestinationId)?.s3DestinationId ||
			data.destinations[0]?.id ||
			'';
		recoveryKey = '';
		actionOpen = true;
	}

	async function openRestoreFiles(backup: SystemBackupRun) {
		restoreFilesTarget = backup;
		restoreFilesRecoveryKey = '';
		restoreFilesProvider = null;
		restoreFilesSelectedPaths = [];
		restoreFilesSelectAll = false;
		restoreFilesSearch = '';
		restoreFilesLoaded = false;
		restoreFilesOpen = true;
		if (policyCollection.recoveryKeyStored) loadRestoreFiles();
	}

	function closeRestoreFiles() {
		restoreFilesOpen = false;
		restoreFilesTarget = null;
		restoreFilesRecoveryKey = '';
		restoreFilesProvider = null;
		restoreFilesSelectedPaths = [];
		restoreFilesSelectAll = false;
		restoreFilesSearch = '';
		restoreFilesLoaded = false;
	}

	function updateRestoreFilesRecoveryKey(value: string) {
		restoreFilesRecoveryKey = value;
		restoreFilesProvider = null;
		restoreFilesSelectedPaths = [];
		restoreFilesSelectAll = false;
		restoreFilesSearch = '';
		restoreFilesLoaded = false;
	}

	function loadRestoreFiles() {
		if (!restoreFilesTarget || restoreFilesKeyInvalid) return;
		restoreFilesSelectedPaths = [];
		restoreFilesSelectAll = false;
		restoreFilesSearch = '';
		const backupID = restoreFilesTarget.id;
		const recoveryKey = restoreFilesRecoveryKey.trim();
		restoreFilesProvider = {
			browse: (request) => systemBackupService.browseFiles(backupID, recoveryKey, request)
		};
		restoreFilesLoaded = true;
	}

	async function restoreSelectedFiles() {
		if (
			!restoreFilesTarget ||
			!restoreFilesLoaded ||
			(!restoreFilesSelectAll && restoreFilesSelectedPaths.length === 0) ||
			restoringFiles
		)
			return;
		restoringFiles = true;
		try {
			const result = await systemBackupService.restoreFiles(restoreFilesTarget.id, restoreFilesRecoveryKey.trim(), {
				paths: restoreFilesSelectedPaths,
				selectAll: restoreFilesSelectAll,
				search: restoreFilesSelectAll ? restoreFilesSearch.trim() : undefined
			});
			const projectQueryKey = queryKeys.projects.environment('0');
			await queryClient.cancelQueries({ queryKey: projectQueryKey });
			queryClient.removeQueries({ queryKey: projectQueryKey });
			toast.success(m.system_backups_restore_selection_success(), activityToastOptions(extractActivityId(result)));
			closeRestoreFiles();
			await refresh();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_backups_restore_files_failed());
		} finally {
			restoringFiles = false;
		}
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
		return m.system_backups_dialog_description();
	}
	async function refresh() {
		backups = await systemBackupService.list(requestOptions);
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
				await systemBackupService.create(
					backupConfiguration === 'custom'
						? { destination, s3DestinationId: destination === 'local' ? '' : s3DestinationId, recoveryKey }
						: { policyId: backupConfiguration, recoveryKey }
				);
				toast.success(m.system_backups_created());
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
				{ label: m.jobs_schedule(), onclick: () => openPolicy() },
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
		contentClass="sm:max-w-[560px]"
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
		contentClass="sm:max-w-[560px]"
	>
		{#snippet children()}
			<div class="space-y-5 py-2">
				{#if action === 'create'}
					<SelectWithLabel
						id="manual-system-backup-configuration"
						value={backupConfiguration}
						onValueChange={(value) => (backupConfiguration = value)}
						label={m.system_backups_backup_configuration()}
						options={configurationOptions}
					/>
					{#if backupConfiguration === 'custom'}
						<SelectWithLabel
							id="manual-system-backup-destination"
							value={destination}
							onValueChange={(value) => (destination = value as SystemBackupDestination)}
							label={m.backups_destination_label()}
							options={destinationOptions}
						/>
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
				{:else if action !== 'delete' && !policyCollection.recoveryKeyStored}
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

{#snippet restoreFilesDialog()}
	<ResponsiveDialog
		bind:open={restoreFilesOpen}
		onOpenChange={(open) => {
			if (!open) closeRestoreFiles();
		}}
		title={m.volume_restore_files()}
		description={m.system_backups_restore_files_description()}
		contentClass="sm:max-w-[640px]"
	>
		{#snippet children()}
			<div class="space-y-3 py-2">
				<Alert.Root class="py-2 [&>svg]:top-2">
					<InfoIcon class="size-4" />
					<Alert.Description class="text-xs">
						{m.system_backups_restore_files_lifecycle_info()}
					</Alert.Description>
				</Alert.Root>

				<div class="flex items-end gap-2">
					<div class="min-w-0 flex-1">
						<TextInputWithLabel
							value={restoreFilesRecoveryKey}
							onChange={updateRestoreFilesRecoveryKey}
							error={restoreFilesKeyError || null}
							label={m.system_backups_recovery_key()}
							description={policyCollection.recoveryKeyStored
								? m.system_backups_recovery_key_saved_description()
								: m.system_backups_recovery_key_enter_description()}
							type="password"
							autocomplete="current-password"
						/>
					</div>
					<ArcaneButton
						action="inspect"
						customLabel={m.system_backups_load_files()}
						onclick={loadRestoreFiles}
						disabled={restoreFilesKeyInvalid}
					/>
				</div>

				{#if restoreFilesProvider}
					<BackupFilePicker
						provider={restoreFilesProvider}
						bind:selectedPaths={restoreFilesSelectedPaths}
						bind:selectAll={restoreFilesSelectAll}
						bind:search={restoreFilesSearch}
					/>

					<Alert.Root variant="warning" class="py-2 [&>svg]:top-2">
						<AlertIcon class="size-4" />
						<Alert.Description class="text-xs">
							{m.volumes_backup_overwrite_warning()}
						</Alert.Description>
					</Alert.Root>
				{/if}
			</div>
		{/snippet}

		{#snippet footer()}
			<ArcaneButton action="cancel" onclick={closeRestoreFiles} disabled={restoringFiles} />
			<ArcaneButton
				action="confirm"
				customLabel={m.volume_restore_files()}
				onclick={restoreSelectedFiles}
				loading={restoringFiles}
				disabled={restoringFiles || !restoreFilesLoaded || (!restoreFilesSelectAll && restoreFilesSelectedPaths.length === 0)}
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
				{#if policyCollection.policies.length}
					<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground sm:grid-cols-2 xl:grid-cols-3">
						{#each policyCollection.policies as policy (policy.id)}
							<BackupPolicyCard {policy} onEdit={() => openPolicy(policy.id)} editDisabled={isReadOnly} />
						{/each}
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">{m.system_backups_no_schedules()}</p>
				{/if}
			</div>

			<SystemBackupTable
				bind:backups
				bind:requestOptions
				onChanged={(options) => systemBackupService.list(options)}
				onRestore={(item) => openAction('restore', item)}
				onRestoreFiles={openRestoreFiles}
				onUpload={(item) => openAction('upload', item)}
				onDelete={(item) => openAction('delete', item)}
			/>
		</div>
	{/snippet}
	{#snippet additionalContent()}
		<BackupPolicyDialog
			bind:open={policyOpen}
			idPrefix="system-backup-policy"
			policies={policyCollection.policies}
			policyId={editingPolicyId}
			addTitle={m.system_backups_add_schedule()}
			description={m.system_backups_schedule_description()}
			enabledDescription={m.system_backups_enabled_description()}
			enabledError={policyCollection.recoveryKeyStored ? null : m.system_backups_recovery_key_schedule_required()}
			defaultSchedule="0 0 3 * * *"
			defaultEnabled={policyCollection.recoveryKeyStored}
			destinations={data.destinations}
			updatePolicies={async (policies) => (await systemBackupService.updatePolicies(policies)).policies}
			messages={{
				saved: m.system_backups_policy_saved(),
				saveFailed: m.system_backups_policy_failed(),
				removed: m.system_backups_schedule_removed()
			}}
			onSaved={(policies) => (policyCollection = { ...policyCollection, policies })}
		/>

		{@render recoveryKeyDialog()}
		{@render actionDialog()}
		{@render restoreFilesDialog()}
	{/snippet}
</SettingsPageLayout>
