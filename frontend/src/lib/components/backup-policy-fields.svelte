<script lang="ts">
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import type { BackupDestination, BackupPolicyForm } from '#lib/types/backup.js';
	import type { S3Destination } from '#lib/types/s3-destination.js';
	import { backupDestinationOptions, s3DestinationOptions } from '#lib/utils/backups.js';
	import * as m from '#lib/paraglide/messages.js';

	let {
		idPrefix,
		form,
		destinations,
		scheduleError,
		retentionError,
		destinationError,
		enabledError,
		enabledDescription,
		schedulePlaceholder,
		showStopContainers = false,
		destinationsLoading = false,
		destinationReadOnly = false,
		destinationsLoadError = null,
		onChange
	}: {
		idPrefix: string;
		form: BackupPolicyForm;
		destinations: S3Destination[];
		scheduleError?: string | null;
		retentionError?: string | null;
		destinationError?: string | null;
		enabledError?: string | null;
		enabledDescription: string;
		schedulePlaceholder: string;
		showStopContainers?: boolean;
		destinationsLoading?: boolean;
		/** Keeps the configured destination as-is when the user cannot list destinations. */
		destinationReadOnly?: boolean;
		destinationsLoadError?: string | null;
		onChange: (values: Partial<BackupPolicyForm>) => void;
	} = $props();

	const keepsConfiguredS3 = $derived(destinationReadOnly && form.destination !== 'local');
	const destinationOptions = $derived(backupDestinationOptions(destinations.length > 0 || keepsConfiguredS3, true));
	const s3Options = $derived.by(() => {
		const options = s3DestinationOptions(destinations);
		if (form.s3DestinationId && !options.some((item) => item.value === form.s3DestinationId)) {
			options.push({ label: form.s3DestinationId, value: form.s3DestinationId, description: '' });
		}
		return options;
	});
</script>

<div class="space-y-5">
	<LabeledSwitch
		id={`${idPrefix}-enabled`}
		checked={form.enabled}
		onCheckedChange={(enabled) => onChange({ enabled })}
		label={m.backups_enabled()}
		description={enabledDescription}
	/>
	{#if enabledError}<p class="text-sm text-destructive">{enabledError}</p>{/if}
	<TextInputWithLabel
		id={`${idPrefix}-schedule`}
		value={form.schedule}
		onChange={(schedule) => onChange({ schedule })}
		error={scheduleError}
		label={m.jobs_schedule()}
		description={m.backups_schedule_description()}
		helpText={scheduleError ? undefined : m.jobs_cron_expression_help()}
		reserveHelpTextSpace
		placeholder={schedulePlaceholder}
	/>
	<TextInputWithLabel
		id={`${idPrefix}-retention`}
		value={form.retentionCount}
		onChange={(retentionCount) => onChange({ retentionCount: Number(retentionCount) })}
		error={retentionError}
		label={m.backups_retention_label()}
		description={m.backups_retention_description()}
		reserveHelpTextSpace
		type="number"
	/>
	{#if showStopContainers}
		<LabeledSwitch
			id={`${idPrefix}-stop-containers`}
			checked={form.stopContainers ?? false}
			onCheckedChange={(stopContainers) => onChange({ stopContainers })}
			label={m.volume_backup_stop_containers()}
			description={m.volume_backup_stop_containers_description()}
		/>
	{/if}
	<SelectWithLabel
		id={`${idPrefix}-destination`}
		value={form.destination}
		onValueChange={(destination) => onChange({ destination: destination as BackupDestination })}
		label={m.backups_destination_label()}
		description={m.volume_backup_destination_description()}
		options={destinationOptions}
		disabled={destinationReadOnly}
	/>
	{#if destinationReadOnly}
		<p class="text-xs text-muted-foreground">{m.volume_backup_destination_read_only()}</p>
	{/if}
	{#if destinationsLoadError}
		<p class="text-sm text-destructive">{m.s3_destinations_load_failed()}: {destinationsLoadError}</p>
	{/if}
	{#if form.destination !== 'local'}
		<SelectWithLabel
			id={`${idPrefix}-s3-destination`}
			value={form.s3DestinationId}
			onValueChange={(s3DestinationId) => onChange({ s3DestinationId })}
			label={m.volume_backup_s3_destination_label()}
			description={m.volume_backup_s3_destination_description()}
			error={destinationError}
			disabled={destinationsLoading || destinationReadOnly}
			options={s3Options}
		/>
	{/if}
</div>
