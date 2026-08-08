<script lang="ts" generics="TPolicy extends BackupPolicy">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import BackupPolicyFields from '#lib/components/backup-policy-fields.svelte';
	import type { BackupPolicy, BackupPolicyForm, BackupPolicyUpdate } from '#lib/types/backup';
	import type { S3Destination } from '#lib/types/s3-destination';
	import { s3DestinationService } from '#lib/services/s3-destination-service';
	import { backupDestinationFromFlags, backupPolicyDestinationValues } from '#lib/utils/backups';
	import { toast } from 'svelte-sonner';
	import * as m from '#lib/paraglide/messages.js';

	type PolicyForm = BackupPolicyForm & { id: string; serverError?: string };

	let {
		open = $bindable(),
		idPrefix,
		policies,
		policyId,
		addTitle,
		description,
		enabledDescription,
		enabledError = null,
		defaultSchedule,
		defaultEnabled = true,
		showStopContainers = false,
		destinations,
		updatePolicies,
		messages,
		onSaved
	}: {
		open: boolean;
		idPrefix: string;
		policies: TPolicy[];
		policyId?: string;
		addTitle: string;
		description: string;
		enabledDescription: string;
		enabledError?: string | null;
		defaultSchedule: string;
		defaultEnabled?: boolean;
		showStopContainers?: boolean;
		destinations?: S3Destination[];
		updatePolicies: (policies: BackupPolicyUpdate[]) => Promise<TPolicy[]>;
		messages: { saved: string; saveFailed: string; removed: string };
		onSaved: (policies: TPolicy[]) => void;
	} = $props();

	let saving = $state(false);
	let deleting = $state(false);
	let loadedDestinations = $state<S3Destination[]>([]);
	let destinationsLoading = $state(false);
	let form = $state<PolicyForm>(newPolicy());
	const destinationList = $derived(destinations ?? loadedDestinations);
	const scheduleError = $derived(
		!form.schedule.trim()
			? m.jobs_cron_required()
			: form.schedule.trim().split(/\s+/).length !== 6
				? m.jobs_cron_invalid()
				: (form.serverError ?? null)
	);
	const retentionError = $derived(
		Number.isInteger(Number(form.retentionCount)) && form.retentionCount >= 0 && form.retentionCount <= 3650
			? null
			: m.volume_backup_retention_invalid()
	);
	const destinationError = $derived.by(() => {
		if (form.destination === 'local' || destinationsLoading) return null;
		if (!form.s3DestinationId) return m.volume_backup_s3_destination_required();
		if (!destinations && !loadedDestinations.some((item) => item.id === form.s3DestinationId))
			return m.volume_backup_destination_unavailable();
		return null;
	});
	const activeEnabledError = $derived(form.enabled ? enabledError : null);
	const formInvalid = $derived(Boolean(scheduleError || retentionError || destinationError || activeEnabledError));

	function newPolicy(): PolicyForm {
		return {
			id: '',
			enabled: defaultEnabled,
			schedule: defaultSchedule,
			retentionCount: 7,
			stopContainers: false,
			destination: 'local',
			s3DestinationId: ''
		};
	}

	async function loadDestinations() {
		destinationsLoading = true;
		try {
			loadedDestinations = await s3DestinationService.listAll();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.s3_destinations_load_failed());
		} finally {
			destinationsLoading = false;
		}
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
					stopContainers: policy.stopContainers ?? false,
					s3DestinationId: policy.s3DestinationId || '',
					destination: backupDestinationFromFlags(policy.localEnabled, policy.s3Enabled)
				}
			: newPolicy();
		if (!destinations) void loadDestinations();
	});

	function updateForm(values: Partial<BackupPolicyForm>) {
		form = { ...form, ...values, serverError: undefined };
	}

	function policyPayload(policy: BackupPolicy): BackupPolicyUpdate {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId ?? '',
			...(showStopContainers ? { stopContainers: policy.stopContainers ?? false } : {})
		};
	}

	async function savePolicies() {
		if (formInvalid) return;
		saving = true;
		try {
			const current: BackupPolicyUpdate = {
				id: form.id,
				enabled: form.enabled,
				schedule: form.schedule,
				retentionCount: Number(form.retentionCount),
				...backupPolicyDestinationValues(form.destination, form.s3DestinationId),
				...(showStopContainers ? { stopContainers: form.stopContainers ?? false } : {})
			};
			const existing = policies.map(policyPayload);
			const next = policyId ? existing.map((policy) => (policy.id === policyId ? current : policy)) : [...existing, current];
			onSaved(await updatePolicies(next));
			open = false;
			toast.success(messages.saved);
		} catch (error) {
			const message = error instanceof Error ? error.message : messages.saveFailed;
			if (/cron|schedule/i.test(message)) form = { ...form, serverError: message };
			else toast.error(message);
		} finally {
			saving = false;
		}
	}

	async function deletePolicy() {
		if (!policyId) return;
		deleting = true;
		try {
			onSaved(await updatePolicies(policies.filter((policy) => policy.id !== policyId).map(policyPayload)));
			open = false;
			toast.success(messages.removed);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : messages.saveFailed);
		} finally {
			deleting = false;
		}
	}
</script>

<ResponsiveDialog bind:open title={policyId ? m.jobs_edit_schedule() : addTitle} {description} contentClass="sm:max-w-[720px]">
	{#snippet children()}
		<div class="space-y-4 py-2">
			<BackupPolicyFields
				{idPrefix}
				{form}
				destinations={destinationList}
				{scheduleError}
				{retentionError}
				{destinationError}
				enabledError={activeEnabledError}
				{enabledDescription}
				schedulePlaceholder={defaultSchedule}
				{showStopContainers}
				{destinationsLoading}
				onChange={updateForm}
			/>
		</div>
	{/snippet}
	{#snippet footer()}
		{#if policyId}
			<ArcaneButton
				action="remove"
				customLabel={m.backups_remove_schedule()}
				onclick={deletePolicy}
				loading={deleting}
				disabled={saving || deleting}
			/>
		{/if}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
		<ArcaneButton action="save" onclick={savePolicies} loading={saving} disabled={saving || deleting || formInvalid} />
	{/snippet}
</ResponsiveDialog>
