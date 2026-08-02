<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import type { UpdateVolumeBackupPolicy, VolumeBackupPolicy } from '#lib/types/shared';
	import type { S3Destination } from '#lib/types/s3-destination';
	import { s3DestinationService } from '#lib/services/s3-destination-service';
	import { volumeBackupService } from '#lib/services/volume-backup-service';
	import { toast } from 'svelte-sonner';
	import * as m from '#lib/paraglide/messages.js';

	type PolicyForm = UpdateVolumeBackupPolicy & { destination: 'local' | 's3' | 'local_s3'; serverError?: string };

	let {
		open = $bindable(),
		volumeName,
		policies,
		policyId,
		onSaved
	}: {
		open: boolean;
		volumeName: string;
		policies: VolumeBackupPolicy[];
		policyId?: string;
		onSaved: (policies: VolumeBackupPolicy[]) => void;
	} = $props();

	let saving = $state(false);
	let deleting = $state(false);
	let s3Destinations = $state<S3Destination[]>([]);
	let s3DestinationsLoading = $state(false);
	let forms = $state<PolicyForm[]>([]);

	const destinationOptions = $derived([
		{ label: m.volume_backup_destination_local(), value: 'local', description: m.volume_backup_destination_local_description() },
		...(s3Destinations.length
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
	const s3DestinationOptions = $derived(
		s3Destinations.map((item) => ({ label: item.name, value: item.id, description: item.bucket }))
	);
	const formInvalid = $derived(
		forms.some((_, index) => Boolean(scheduleError(index) || retentionError(index) || destinationError(index)))
	);

	function newPolicy(): PolicyForm {
		return {
			id: '',
			enabled: true,
			schedule: '0 0 2 * * *',
			retentionCount: 7,
			stopContainers: false,
			localEnabled: true,
			s3Enabled: false,
			s3DestinationId: '',
			destination: 'local'
		};
	}

	function scheduleError(index: number): string | null {
		const form = forms[index];
		if (!form) return null;
		if (!form.schedule.trim()) return m.jobs_cron_required();
		if (form.schedule.trim().split(/\s+/).length !== 6) return m.jobs_cron_invalid();
		return form.serverError ?? null;
	}

	function retentionError(index: number): string | null {
		const value = Number(forms[index]?.retentionCount);
		return Number.isInteger(value) && value >= 0 && value <= 3650 ? null : m.volume_backup_retention_invalid();
	}

	function destinationError(index: number): string | null {
		const form = forms[index];
		if (!form || form.destination === 'local' || s3DestinationsLoading) return null;
		if (!form.s3DestinationId) return m.volume_backup_s3_destination_required();
		return s3Destinations.some((item) => item.id === form.s3DestinationId) ? null : m.volume_backup_destination_unavailable();
	}

	async function loadS3Destinations() {
		s3DestinationsLoading = true;
		try {
			s3Destinations = await s3DestinationService.listAll();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.s3_destinations_load_failed());
		} finally {
			s3DestinationsLoading = false;
		}
	}

	$effect(() => {
		if (!open) return;
		const policy = policies.find((item) => item.id === policyId);
		forms = policy
			? [
					{
						id: policy.id,
						enabled: policy.enabled,
						schedule: policy.schedule,
						retentionCount: policy.retentionCount,
						stopContainers: policy.stopContainers,
						localEnabled: policy.localEnabled,
						s3Enabled: policy.s3Enabled,
						s3DestinationId: policy.s3DestinationId || '',
						destination: policy.s3Enabled ? (policy.localEnabled ? 'local_s3' : 's3') : 'local'
					}
				]
			: [newPolicy()];
		void loadS3Destinations();
	});

	function updateForm(index: number, values: Partial<PolicyForm>) {
		forms = forms.map((form, itemIndex) => (itemIndex === index ? { ...form, ...values, serverError: undefined } : form));
	}

	function policyUpdatePayload(policy: VolumeBackupPolicy): UpdateVolumeBackupPolicy {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			stopContainers: policy.stopContainers,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId
		};
	}

	async function savePolicies() {
		if (formInvalid) return;
		saving = true;
		try {
			const payload = forms.map(({ destination, serverError: _, ...form }) => ({
				...form,
				retentionCount: Number(form.retentionCount),
				localEnabled: destination !== 's3',
				s3Enabled: destination !== 'local',
				s3DestinationId: destination === 'local' ? '' : form.s3DestinationId
			}));
			const currentPolicy = payload[0];
			if (!currentPolicy) return;
			const existingPolicies = policies.map(policyUpdatePayload);
			const nextPolicies = policyId
				? existingPolicies.map((policy) => (policy.id === policyId ? currentPolicy : policy))
				: [...existingPolicies, currentPolicy];
			const updated = await volumeBackupService.updatePolicies(volumeName, nextPolicies);
			onSaved(updated.policies);
			open = false;
			toast.success(m.volume_backup_policy_saved());
		} catch (error) {
			const message = error instanceof Error ? error.message : m.volume_backup_policy_save_failed();
			if (/cron|schedule/i.test(message) && forms.length) updateForm(0, { serverError: message });
			else toast.error(message);
		} finally {
			saving = false;
		}
	}

	async function deletePolicy() {
		if (!policyId) return;
		deleting = true;
		try {
			const remainingPolicies = policies.filter((policy) => policy.id !== policyId).map(policyUpdatePayload);
			const updated = await volumeBackupService.updatePolicies(volumeName, remainingPolicies);
			onSaved(updated.policies);
			open = false;
			toast.success(m.volume_backup_schedule_removed());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.volume_backup_policy_save_failed());
		} finally {
			deleting = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={policyId ? m.volume_backup_edit_schedule() : m.volume_backup_add_schedule()}
	description={m.volume_backup_policy_description()}
	contentClass="sm:max-w-[760px]"
>
	{#snippet children()}
		<div class="space-y-4 py-2">
			{#each forms as form, index (form.id || index)}
				<div class="space-y-5">
					<LabeledSwitch
						id={`volume-backup-policy-enabled-${index}`}
						checked={form.enabled}
						onCheckedChange={(checked) => updateForm(index, { enabled: checked })}
						label={m.volume_backup_policy_enabled()}
						description={m.volume_backup_policy_enabled_description()}
					/>
					<TextInputWithLabel
						value={form.schedule}
						error={scheduleError(index)}
						onChange={(schedule) => updateForm(index, { schedule })}
						label={m.jobs_schedule()}
						description={m.backups_schedule_description()}
						helpText={scheduleError(index) ? undefined : m.jobs_cron_expression_help()}
						reserveHelpTextSpace
						placeholder="0 0 2 * * *"
					/>
					<TextInputWithLabel
						value={form.retentionCount}
						error={retentionError(index)}
						onChange={(retentionCount) => updateForm(index, { retentionCount: Number(retentionCount) })}
						label={m.backups_retention_label()}
						description={m.backups_retention_description()}
						reserveHelpTextSpace
						type="number"
					/>
					<LabeledSwitch
						id={`volume-backup-policy-stop-containers-${index}`}
						checked={form.stopContainers}
						onCheckedChange={(checked) => updateForm(index, { stopContainers: checked })}
						label={m.volume_backup_stop_containers()}
						description={m.volume_backup_stop_containers_description()}
					/>
					<SelectWithLabel
						id={`volume-backup-policy-destination-${index}`}
						value={form.destination}
						onValueChange={(destination) => updateForm(index, { destination: destination as PolicyForm['destination'] })}
						label={m.volume_backup_destination_label()}
						description={m.volume_backup_destination_description()}
						error={destinationError(index)}
						options={destinationOptions}
					/>
					{#if form.destination !== 'local'}
						<SelectWithLabel
							id={`volume-backup-policy-s3-destination-${index}`}
							value={form.s3DestinationId}
							onValueChange={(s3DestinationId) => updateForm(index, { s3DestinationId })}
							label={m.volume_backup_s3_destination_label()}
							description={m.volume_backup_s3_destination_description()}
							error={destinationError(index)}
							disabled={s3DestinationsLoading}
							options={s3DestinationOptions}
						/>
					{/if}
				</div>
			{/each}
		</div>
	{/snippet}
	{#snippet footer()}
		{#if policyId}
			<ArcaneButton
				action="remove"
				customLabel={m.volume_backup_remove_schedule()}
				onclick={deletePolicy}
				loading={deleting}
				disabled={saving || deleting}
			/>
		{/if}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
		<ArcaneButton action="save" onclick={savePolicies} loading={saving} disabled={saving || deleting || formInvalid} />
	{/snippet}
</ResponsiveDialog>
