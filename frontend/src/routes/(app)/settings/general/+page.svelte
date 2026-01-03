<script lang="ts">
	import { getContext } from 'svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';
	import { m } from '$lib/paraglide/messages';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsIcon } from '$lib/icons';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Label } from '$lib/components/ui/label';
	import { createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import type { JobSchedules } from '$lib/types/job-schedule.type';

	let { data } = $props();

	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);
	let isLoading = $state(false);

	// Track the last saved schedules separately so we can compute hasChanges and reset.
	let savedSchedules = $state<JobSchedules>(untrack(() => data.jobSchedules));

	const formSchema = z.object({
		environmentHealthInterval: z.coerce.number().int().min(1).max(60),
		eventCleanupInterval: z.coerce.number().int().min(5).max(10080),
		analyticsHeartbeatInterval: z.coerce.number().int().min(60).max(43200)
	});

	const form = createForm(
		formSchema,
		untrack(() => savedSchedules)
	);
	const formInputs = form.inputs;

	const hasChanges = $derived.by(() => {
		const inputs = $formInputs;
		return (
			inputs.environmentHealthInterval.value !== savedSchedules.environmentHealthInterval ||
			inputs.eventCleanupInterval.value !== savedSchedules.eventCleanupInterval ||
			inputs.analyticsHeartbeatInterval.value !== savedSchedules.analyticsHeartbeatInterval
		);
	});

	// Integrate with Settings layout Save/Reset buttons via context.
	type SettingsFormState = {
		hasChanges: boolean;
		isLoading: boolean;
		saveFunction: (() => Promise<void>) | null;
		resetFunction: (() => void) | null;
	};
	let formState: SettingsFormState | null = null;
	try {
		formState = getContext('settingsFormState') as SettingsFormState;
	} catch {
		// Context not available (shouldn't happen in settings routes)
	}

	function resetForm() {
		formInputs.update((inputs) => {
			inputs.environmentHealthInterval.value = savedSchedules.environmentHealthInterval;
			inputs.environmentHealthInterval.error = null;
			inputs.eventCleanupInterval.value = savedSchedules.eventCleanupInterval;
			inputs.eventCleanupInterval.error = null;
			inputs.analyticsHeartbeatInterval.value = savedSchedules.analyticsHeartbeatInterval;
			inputs.analyticsHeartbeatInterval.error = null;
			return inputs;
		});
	}

	async function save() {
		if (isReadOnly) {
			return;
		}

		const values = form.validate();
		if (!values) {
			toast.error('Please check the form for errors');
			return;
		}

		isLoading = true;
		const result = await tryCatch(jobScheduleService.updateJobSchedules(values));
		isLoading = false;

		if (result.error) {
			console.error('Failed to update job schedules:', result.error);
			toast.error(result.error.message || 'Failed to update job schedules');
			return;
		}

		savedSchedules = result.data;
		resetForm();
		toast.success('Job schedules updated');
	}

	$effect(() => {
		if (!formState) return;
		formState.hasChanges = hasChanges;
		formState.isLoading = isLoading;
		formState.saveFunction = save;
		formState.resetFunction = resetForm;
	});
</script>

<SettingsPageLayout
	title="Job Schedule"
	description="Configure how often Arcane background jobs run. Changes apply immediately."
	icon={SettingsIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly || isLoading} class="relative space-y-8">
			<!-- Monitoring -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Monitoring</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.environments_health_check_title()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.environments_health_check_description()}</p>
							</div>
							<div class="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.environmentHealthInterval.value}
									error={$formInputs.environmentHealthInterval.error}
									label={m.environments_health_check_interval_label()}
									placeholder="2"
									helpText={m.environments_health_check_interval_description()}
									type="number"
								/>
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Maintenance -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Maintenance</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">Event Cleanup</Label>
								<p class="text-muted-foreground mt-1 text-sm">How often Arcane deletes events older than 36 hours</p>
							</div>
							<div class="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.eventCleanupInterval.value}
									error={$formInputs.eventCleanupInterval.error}
									label="Event Cleanup Interval (minutes)"
									placeholder="360"
									helpText="Run every 5–10080 minutes"
									type="number"
								/>
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Telemetry -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Telemetry</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">Analytics Heartbeat</Label>
								<p class="text-muted-foreground mt-1 text-sm">Only runs in production, and only if analytics are enabled</p>
							</div>
							<div class="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.analyticsHeartbeatInterval.value}
									error={$formInputs.analyticsHeartbeatInterval.error}
									label="Analytics Heartbeat Interval (minutes)"
									placeholder="1440"
									helpText="Minimum 60 minutes"
									type="number"
								/>
							</div>
						</div>
					</div>
				</div>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
