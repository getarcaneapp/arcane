<script lang="ts">
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts';
	import { BackupIcon, EditIcon, LockIcon } from '$lib/icons';
	import { Badge } from '$lib/components/ui/badge';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { systemBackupService } from '$lib/services/system-backup-service';
	import { formatDateTimeShort } from '$lib/utils/formatting';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import type { SystemBackupDestination, SystemBackupRun } from '$lib/types/system-backup';
	import * as m from '$lib/paraglide/messages.js';
	import SystemBackupTable from './system-backup-table.svelte';
	import SystemBackupPolicyDialog from './system-backup-policy-dialog.svelte';

	let { data } = $props();
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
	let savingKey = $state(false);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const destinationOptions = $derived([
		{ label: m.volume_backup_destination_local(), value: 'local' },
		...(data.destinations.length
			? [
					{ label: m.volume_backup_destination_s3(), value: 's3' },
					{ label: m.volume_backup_destination_local_s3(), value: 'local_s3' }
				]
			: [])
	]);
	const s3Options = $derived(data.destinations.map((item) => ({ label: item.name, value: item.id, description: item.bucket })));
	const configurationOptions = $derived([
		{
			label: m.system_backups_custom_configuration(),
			value: 'custom',
			description: m.system_backups_custom_configuration_description()
		},
		...policyCollection.policies.map((item) => ({ label: item.schedule, value: item.id, description: policyDestination(item) }))
	]);
	const keyError = $derived(
		recoveryKey.length > 0 && recoveryKey.trim().length < 16 ? m.system_backups_recovery_key_required() : ''
	);
	const actionKeyInvalid = $derived(!policyCollection.recoveryKeyStored && recoveryKey.trim().length < 16);
	const newKeyError = $derived(
		newRecoveryKey.length > 0 && newRecoveryKey.trim().length < 16 ? m.system_backups_recovery_key_required() : ''
	);
	const newKeyInvalid = $derived(newRecoveryKey.trim().length < 16);
	const destinationError = $derived(
		backupConfiguration === 'custom' &&
			((action === 'create' && destination !== 'local') || action === 'upload' || action === 'discover') &&
			!s3DestinationId
			? m.volume_backup_s3_destination_required()
			: ''
	);
	const invalid = $derived(Boolean(actionKeyInvalid || keyError || destinationError));

	function policyDestination(policy: (typeof policyCollection.policies)[number]) {
		const label = policy.s3Enabled
			? policy.localEnabled
				? m.backups_destination_local_s3()
				: m.backups_destination_s3()
			: m.backups_destination_local();
		return policy.s3DestinationName ? `${label} · ${policy.s3DestinationName}` : label;
	}

	function backupStatusLabel(status: SystemBackupRun['status']) {
		if (status === 'succeeded') return m.volume_backup_status_succeeded();
		if (status === 'failed') return m.volume_backup_status_failed();
		return m.volume_backup_status_running();
	}

	function openPolicy(id?: string) {
		editingPolicyId = id;
		policyOpen = true;
	}

	function openRecoveryKey() {
		newRecoveryKey = '';
		keyOpen = true;
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
	function dialogTitle() {
		if (action === 'restore') return m.system_backups_restore_title();
		if (action === 'upload') return m.system_backups_upload_title();
		if (action === 'delete') return m.system_backups_delete_title();
		if (action === 'discover') return m.system_backups_discover_title();
		return m.system_backups_create();
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

	async function saveRecoveryKey() {
		if (newKeyInvalid) return;
		savingKey = true;
		try {
			await systemBackupService.setRecoveryKey(newRecoveryKey);
			policyCollection = { ...policyCollection, recoveryKeyStored: true };
			newRecoveryKey = '';
			keyOpen = false;
			toast.success(m.system_backups_recovery_key_saved());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_backups_recovery_key_save_failed());
		} finally {
			savingKey = false;
		}
	}

	async function submitAction() {
		if (invalid) return;
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
		{ id: 'schedule', action: 'base', label: m.system_backups_add_schedule(), onclick: () => openPolicy(), disabled: isReadOnly },
		{
			id: 'discover',
			action: 'base',
			label: m.system_backups_discover(),
			onclick: () => openAction('discover'),
			disabled: isReadOnly || data.destinations.length === 0
		},
		{
			id: 'create',
			action: 'create',
			label: m.system_backups_create(),
			onclick: () => openAction('create'),
			disabled: isReadOnly
		}
	]);
</script>

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
			<div class="flex items-center justify-between rounded-md border px-3 py-2">
				<div class="flex items-center gap-2">
					<LockIcon class="size-4 text-muted-foreground" />
					<span class="text-sm font-medium">{m.system_backups_recovery_key()}</span>
					<Badge variant={policyCollection.recoveryKeyStored ? 'green' : 'gray'}
						>{policyCollection.recoveryKeyStored
							? m.system_backups_key_configured()
							: m.system_backups_key_not_configured()}</Badge
					>
				</div>
				<ArcaneButton
					action="edit"
					size="sm"
					customLabel={policyCollection.recoveryKeyStored
						? m.system_backups_replace_recovery_key()
						: m.system_backups_setup_recovery_key()}
					onclick={openRecoveryKey}
					disabled={isReadOnly}
				/>
			</div>

			<div class="space-y-2">
				<h2 class="text-lg font-semibold">{m.system_backups_schedules()}</h2>
				{#if policyCollection.policies.length}
					<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground sm:grid-cols-2 xl:grid-cols-3">
						{#each policyCollection.policies as policy (policy.id)}
							<div
								class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-1.5 gap-y-0.5 rounded-md border px-2 py-1"
							>
								<Badge variant={policy.enabled ? 'green' : 'gray'}
									>{policy.enabled ? m.common_enabled() : m.common_disabled()}</Badge
								>
								<code class="truncate">{policy.schedule}</code>
								<div class="flex items-center gap-1.5 justify-self-end">
									<Badge variant="blue"
										>{policy.s3Enabled
											? policy.localEnabled
												? m.backups_destination_local_s3()
												: m.backups_destination_s3()
											: m.backups_destination_local()}</Badge
									>
									<ArcaneButton
										action="edit"
										size="icon"
										icon={EditIcon}
										showLabel={false}
										customLabel={m.system_backups_edit_schedule()}
										onclick={() => openPolicy(policy.id)}
										class="size-7"
										disabled={isReadOnly}
									/>
								</div>
								<div class="col-span-2 flex min-w-0 items-center gap-1.5">
									<span
										>{policy.retentionCount === 0
											? m.volume_backup_retention_all()
											: m.volume_backup_retention_summary({ count: policy.retentionCount })}</span
									>
									{#if policy.lastRun}<span>·</span><span class="truncate"
											>{backupStatusLabel(policy.lastRun.status)} · {formatDateTimeShort(policy.lastRun.createdAt)}</span
										>{/if}
								</div>
								{#if policy.s3DestinationName}<span class="max-w-28 justify-self-end truncate" title={policy.s3DestinationName}
										>{policy.s3DestinationName}</span
									>{/if}
							</div>
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
				onUpload={(item) => openAction('upload', item)}
				onDelete={(item) => openAction('delete', item)}
			/>
		</div>
	{/snippet}
	{#snippet additionalContent()}
		<SystemBackupPolicyDialog
			bind:open={policyOpen}
			policies={policyCollection.policies}
			policyId={editingPolicyId}
			recoveryKeyStored={policyCollection.recoveryKeyStored}
			destinations={data.destinations}
			onSaved={(policies) => (policyCollection = { ...policyCollection, policies })}
		/>

		<ResponsiveDialog
			bind:open={keyOpen}
			title={policyCollection.recoveryKeyStored ? m.system_backups_replace_recovery_key() : m.system_backups_setup_recovery_key()}
			description={policyCollection.recoveryKeyStored
				? m.system_backups_replace_recovery_key_description()
				: m.system_backups_recovery_key_description()}
			contentClass="sm:max-w-[560px]"
		>
			{#snippet children()}<div class="py-2">
					<TextInputWithLabel
						value={newRecoveryKey}
						onChange={(value) => (newRecoveryKey = value)}
						error={newKeyError || null}
						label={m.system_backups_recovery_key()}
						type="password"
						autocomplete="new-password"
					/>
				</div>{/snippet}
			{#snippet footer()}<ArcaneButton action="cancel" onclick={() => (keyOpen = false)} disabled={savingKey} /><ArcaneButton
					action="save"
					onclick={saveRecoveryKey}
					loading={savingKey}
					disabled={savingKey || newKeyInvalid}
				/>{/snippet}
		</ResponsiveDialog>

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
						{#if backupConfiguration === 'custom'}<SelectWithLabel
								id="manual-system-backup-destination"
								value={destination}
								onValueChange={(value) => (destination = value as SystemBackupDestination)}
								label={m.system_backups_destination()}
								options={destinationOptions}
							/>{/if}
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
					<TextInputWithLabel
						value={recoveryKey}
						onChange={(value) => (recoveryKey = value)}
						error={keyError || null}
						label={m.system_backups_recovery_key()}
						description={policyCollection.recoveryKeyStored
							? m.system_backups_recovery_key_saved_description()
							: m.system_backups_recovery_key_description()}
						type="password"
						autocomplete="current-password"
					/>
				</div>
			{/snippet}
			{#snippet footer()}<ArcaneButton action="cancel" onclick={() => (actionOpen = false)} disabled={loading} /><ArcaneButton
					action={action === 'delete' ? 'remove' : action === 'restore' ? 'confirm' : action === 'create' ? 'create' : 'save'}
					customLabel={dialogTitle()}
					onclick={submitAction}
					{loading}
					disabled={loading || invalid}
				/>{/snippet}
		</ResponsiveDialog>
	{/snippet}
</SettingsPageLayout>
