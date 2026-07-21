<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import LabeledSwitch from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import type { S3Destination } from '$lib/types/s3-destination';
	import type { SystemBackupPolicy, SystemBackupDestination, UpdateSystemBackupPolicy } from '$lib/types/system-backup';
	import { systemBackupService } from '$lib/services/system-backup-service';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';

	type PolicyForm = UpdateSystemBackupPolicy & { destination: SystemBackupDestination; serverError?: string };

	let {
		open = $bindable(),
		policies,
		policyId,
		recoveryKeyStored,
		destinations,
		onSaved
	}: {
		open: boolean;
		policies: SystemBackupPolicy[];
		policyId?: string;
		recoveryKeyStored: boolean;
		destinations: S3Destination[];
		onSaved: (policies: SystemBackupPolicy[]) => void;
	} = $props();

	let saving = $state(false);
	let deleting = $state(false);
	let form = $state<PolicyForm>(newPolicy());

	function newPolicy(): PolicyForm {
		return {
			id: '',
			enabled: recoveryKeyStored,
			schedule: '0 0 3 * * *',
			retentionCount: 7,
			localEnabled: true,
			s3Enabled: false,
			s3DestinationId: '',
			destination: 'local'
		};
	}

	$effect(() => {
		if (!open) return;
		const policy = policies.find((item) => item.id === policyId);
		form = policy
			? {
					id: policy.id,
					enabled: policy.enabled,
					schedule: policy.schedule,
					retentionCount: policy.retentionCount,
					localEnabled: policy.localEnabled,
					s3Enabled: policy.s3Enabled,
					s3DestinationId: policy.s3DestinationId || '',
					destination: policy.s3Enabled ? (policy.localEnabled ? 'local_s3' : 's3') : 'local'
				}
			: newPolicy();
	});

	const destinationOptions = $derived([
		{ label: m.volume_backup_destination_local(), value: 'local', description: m.volume_backup_destination_local_description() },
		...(destinations.length
			? [
					{ label: m.volume_backup_destination_s3(), value: 's3', description: m.volume_backup_destination_s3_description() },
					{
						label: m.volume_backup_destination_local_s3(),
						value: 'local_s3',
						description: m.volume_backup_destination_local_s3_description()
					}
				]
			: [])
	]);
	const s3Options = $derived(destinations.map((item) => ({ label: item.name, value: item.id, description: item.bucket })));
	const scheduleError = $derived(
		!form.schedule.trim() || form.schedule.trim().split(/\s+/).length !== 6 ? m.jobs_cron_invalid() : form.serverError || ''
	);
	const retentionError = $derived(
		Number.isInteger(Number(form.retentionCount)) && form.retentionCount >= 0 && form.retentionCount <= 3650
			? ''
			: m.volume_backup_retention_invalid()
	);
	const destinationError = $derived(
		form.destination !== 'local' && !form.s3DestinationId ? m.volume_backup_s3_destination_required() : ''
	);
	const keyError = $derived(form.enabled && !recoveryKeyStored ? m.system_backups_recovery_key_schedule_required() : '');
	const invalid = $derived(Boolean(scheduleError || retentionError || destinationError || keyError));

	function payload(policy: SystemBackupPolicy): UpdateSystemBackupPolicy {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId
		};
	}

	async function save() {
		if (invalid) return;
		saving = true;
		try {
			const { destination, serverError: _, ...values } = form;
			const current: UpdateSystemBackupPolicy = {
				...values,
				retentionCount: Number(values.retentionCount),
				localEnabled: destination !== 's3',
				s3Enabled: destination !== 'local',
				s3DestinationId: destination === 'local' ? '' : values.s3DestinationId
			};
			const existing = policies.map(payload);
			const next = policyId ? existing.map((item) => (item.id === policyId ? current : item)) : [...existing, current];
			const updated = await systemBackupService.updatePolicies(next);
			onSaved(updated.policies);
			open = false;
			toast.success(m.system_backups_policy_saved());
		} catch (error) {
			const message = error instanceof Error ? error.message : m.system_backups_policy_failed();
			if (/cron|schedule/i.test(message)) form = { ...form, serverError: message };
			else toast.error(message);
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!policyId) return;
		deleting = true;
		try {
			const updated = await systemBackupService.updatePolicies(policies.filter((item) => item.id !== policyId).map(payload));
			onSaved(updated.policies);
			open = false;
			toast.success(m.system_backups_schedule_removed());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_backups_policy_failed());
		} finally {
			deleting = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={policyId ? m.system_backups_edit_schedule() : m.system_backups_add_schedule()}
	description={m.system_backups_schedule_description()}
	contentClass="sm:max-w-[680px]"
>
	{#snippet children()}
		<div class="space-y-5 py-2">
			<LabeledSwitch
				id="system-backup-enabled"
				checked={form.enabled}
				onCheckedChange={(enabled) => (form = { ...form, enabled })}
				label={m.system_backups_enabled()}
				description={m.system_backups_enabled_description()}
			/>
			{#if keyError}<p class="text-sm text-destructive">{keyError}</p>{/if}
			<TextInputWithLabel
				value={form.schedule}
				onChange={(schedule) => (form = { ...form, schedule, serverError: undefined })}
				error={scheduleError || null}
				label={m.jobs_schedule()}
				description={m.backups_schedule_description()}
				helpText={scheduleError ? undefined : m.jobs_cron_expression_help()}
				reserveHelpTextSpace
				placeholder="0 0 3 * * *"
			/>
			<TextInputWithLabel
				value={form.retentionCount}
				onChange={(retentionCount) => (form = { ...form, retentionCount: Number(retentionCount) })}
				error={retentionError || null}
				label={m.backups_retention_label()}
				description={m.backups_retention_description()}
				reserveHelpTextSpace
				type="number"
			/>
			<SelectWithLabel
				id="system-backup-destination"
				value={form.destination}
				onValueChange={(destination) => (form = { ...form, destination: destination as SystemBackupDestination })}
				label={m.system_backups_destination()}
				options={destinationOptions}
			/>
			{#if form.destination !== 'local'}
				<SelectWithLabel
					id="system-backup-s3"
					value={form.s3DestinationId || ''}
					onValueChange={(s3DestinationId) => (form = { ...form, s3DestinationId })}
					label={m.volume_backup_s3_destination_label()}
					error={destinationError || null}
					options={s3Options}
				/>
			{/if}
		</div>
	{/snippet}
	{#snippet footer()}
		{#if policyId}<ArcaneButton
				action="remove"
				customLabel={m.system_backups_remove_schedule()}
				onclick={remove}
				loading={deleting}
				disabled={saving || deleting}
			/>{/if}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
		<ArcaneButton action="save" onclick={save} loading={saving} disabled={saving || deleting || invalid} />
	{/snippet}
</ResponsiveDialog>
