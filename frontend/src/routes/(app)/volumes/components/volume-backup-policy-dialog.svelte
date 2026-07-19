<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import LabeledSwitch from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import type { UpdateVolumeBackupPolicy, VolumeBackupPolicy } from '$lib/types/shared';
	import type { S3Destination } from '$lib/types/s3-destination';
	import { s3DestinationService } from '$lib/services/s3-destination-service';
	import { volumeBackupService } from '$lib/services/volume-backup-service';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';

	let {
		open = $bindable(),
		volumeName,
		policy,
		onSaved
	}: {
		open: boolean;
		volumeName: string;
		policy: VolumeBackupPolicy;
		onSaved: (policy: VolumeBackupPolicy) => void;
	} = $props();

	let saving = $state(false);
	let s3Destinations = $state<S3Destination[]>([]);
	let s3DestinationsLoading = $state(false);
	let destination = $state<'local' | 's3' | 'local_s3'>('local');
	let scheduleServerError = $state<string | null>(null);
	let form = $state<UpdateVolumeBackupPolicy>({
		enabled: false,
		schedule: '0 0 2 * * *',
		retentionCount: 7,
		stopContainers: false,
		localEnabled: true,
		s3Enabled: false,
		s3DestinationId: ''
	});
	const scheduleError = $derived.by(() => {
		const schedule = form.schedule.trim();
		if (!schedule) return m.jobs_cron_required();
		if (schedule.split(/\s+/).length !== 6) return m.jobs_cron_invalid();
		return scheduleServerError;
	});
	const retentionError = $derived.by(() => {
		const retentionCount = Number(form.retentionCount);
		if (!Number.isInteger(retentionCount) || retentionCount < 0 || retentionCount > 3650) {
			return m.volume_backup_retention_invalid();
		}
		return null;
	});
	const destinationError = $derived.by(() => {
		if (destination === 'local' || s3DestinationsLoading) return null;
		if (!form.s3DestinationId) return m.volume_backup_s3_destination_required();
		if (!s3Destinations.some((item) => item.id === form.s3DestinationId)) {
			return m.volume_backup_destination_unavailable();
		}
		return null;
	});
	const formInvalid = $derived(Boolean(scheduleError || retentionError || destinationError));
	const destinationOptions = $derived([
		{
			label: m.volume_backup_destination_local(),
			value: 'local',
			description: m.volume_backup_destination_local_description()
		},
		...(s3Destinations.length > 0
			? [
					{
						label: m.volume_backup_destination_s3(),
						value: 's3',
						description: m.volume_backup_destination_s3_description()
					},
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
		form = {
			enabled: policy.enabled,
			schedule: policy.schedule || '0 0 2 * * *',
			retentionCount: policy.retentionCount ?? 7,
			stopContainers: policy.stopContainers ?? false,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId || ''
		};
		destination = policy.s3Enabled ? (policy.localEnabled ? 'local_s3' : 's3') : 'local';
		scheduleServerError = null;
		void loadS3Destinations();
	});

	async function savePolicy() {
		if (formInvalid) return;
		saving = true;
		try {
			const payload: UpdateVolumeBackupPolicy = {
				...form,
				retentionCount: Number(form.retentionCount),
				localEnabled: destination !== 's3',
				s3Enabled: destination !== 'local',
				s3DestinationId: destination === 'local' ? '' : form.s3DestinationId
			};
			const updated = await volumeBackupService.updatePolicy(volumeName, payload);
			onSaved(updated);
			open = false;
			toast.success(m.volume_backup_policy_saved());
		} catch (error) {
			const message = error instanceof Error ? error.message : m.volume_backup_policy_save_failed();
			if (/cron|schedule/i.test(message)) {
				scheduleServerError = message;
			} else {
				toast.error(message);
			}
		} finally {
			saving = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={m.volume_backup_policy_title()}
	description={m.volume_backup_policy_description()}
	contentClass="sm:max-w-[760px]"
>
	{#snippet children()}
		<div class="max-h-[70vh] space-y-6 overflow-y-auto py-2 pr-1">
			<div class="space-y-4">
				<LabeledSwitch
					id="volume-backup-policy-enabled"
					bind:checked={form.enabled}
					label={m.volume_backup_policy_enabled()}
					description={m.volume_backup_policy_enabled_description()}
				/>
				<div
					class="grid gap-5 sm:grid-cols-2 sm:[&>div]:grid sm:[&>div]:grid-rows-[3.5rem_2.25rem_1rem] sm:[&>div]:gap-2.5 sm:[&>div]:space-y-0"
				>
					<TextInputWithLabel
						bind:value={form.schedule}
						error={scheduleError}
						onChange={() => (scheduleServerError = null)}
						label={m.jobs_cron_expression()}
						description={m.backups_schedule_description()}
						helpText={scheduleError ? undefined : m.jobs_cron_expression_help()}
						placeholder="0 0 2 * * *"
					/>
					<TextInputWithLabel
						bind:value={form.retentionCount}
						error={retentionError}
						label={m.backups_retention_label()}
						description={m.backups_retention_description()}
						reserveHelpTextSpace={!retentionError}
						type="number"
					/>
				</div>
				<LabeledSwitch
					id="volume-backup-policy-stop-containers my-2"
					bind:checked={form.stopContainers}
					label={m.volume_backup_stop_containers()}
					description={m.volume_backup_stop_containers_description()}
				/>
			</div>

			<div class="border-t pt-5">
				<SelectWithLabel
					id="volume-backup-policy-destination"
					bind:value={destination}
					label={m.volume_backup_destination_label()}
					description={m.volume_backup_destination_description()}
					error={destinationError}
					options={destinationOptions}
				/>
				{#if destination !== 'local'}
					<div class="mt-5">
						<SelectWithLabel
							id="volume-backup-policy-s3-destination"
							bind:value={form.s3DestinationId}
							label={m.volume_backup_s3_destination_label()}
							description={m.volume_backup_s3_destination_description()}
							error={destinationError}
							disabled={s3DestinationsLoading}
							options={s3DestinationOptions}
						/>
					</div>
				{/if}
			</div>
		</div>
	{/snippet}
	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
		<ArcaneButton action="save" onclick={savePolicy} loading={saving} disabled={saving || formInvalid} />
	{/snippet}
</ResponsiveDialog>
