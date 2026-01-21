<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { Button } from '$lib/components/ui/button';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import { Label } from '$lib/components/ui/label';
	import { Input } from '$lib/components/ui/input';
	import { tryCatch } from '$lib/utils/try-catch';
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import type { JobStatus } from '$lib/types/job-schedule.type';

	let {
		job,
		open = $bindable(false),
		onUpdate
	}: {
		job: JobStatus;
		open?: boolean;
		onUpdate?: () => void;
	} = $props();

	let scheduleValue = $state('');
	let error = $state<string | null>(null);
	let isLoading = $state(false);

	const cronExamples = [
		{ label: m.jobs_cron_example_every_15min(), value: '0 */15 * * * *' },
		{ label: m.jobs_cron_example_hourly(), value: '0 0 * * * *' },
		{ label: m.jobs_cron_example_every_6hours(), value: '0 0 */6 * * *' },
		{ label: m.jobs_cron_example_daily(), value: '0 0 0 * * *' }
	];

	function validateCron(value: string): boolean {
		if (!value.trim()) {
			error = m.jobs_cron_required();
			return false;
		}

		const parts = value.trim().split(/\s+/);
		if (parts.length !== 6) {
			error = m.jobs_cron_invalid();
			return false;
		}

		error = null;
		return true;
	}

	async function save() {
		if (!validateCron(scheduleValue)) {
			return;
		}

		if (!job.settingsKey) {
			toast.error(m.jobs_schedule_update_unavailable());
			return;
		}

		isLoading = true;
		const update = { [job.settingsKey]: scheduleValue };
		const result = await tryCatch(jobScheduleService.updateJobSchedules(update));
		isLoading = false;

		if (result.error) {
			toast.error(result.error.message || m.jobs_schedule_update_failed());
			return;
		}

		toast.success(m.jobs_schedule_updated());
		onUpdate?.();
	}

	function useCronExample(value: string) {
		scheduleValue = value;
		error = null;
	}

	function handleOpenChange(isOpen: boolean) {
		if (isOpen) {
			scheduleValue = job.schedule;
			error = null;
		}
	}

	handleOpenChange(open);
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={handleOpenChange}
	title={m.jobs_edit_schedule()}
	description={job.name}
	contentClass="sm:max-w-[500px]"
>
	<div class="space-y-4 py-4">
		<div class="space-y-2">
			<Label for="schedule">{m.jobs_cron_expression()}</Label>
			<Input id="schedule" bind:value={scheduleValue} placeholder="0 */15 * * * *" class={error ? 'border-destructive' : ''} />
			{#if error}
				<p class="text-destructive text-sm">{error}</p>
			{:else}
				<p class="text-muted-foreground text-sm">{m.jobs_cron_expression_help()}</p>
			{/if}
		</div>

		<div class="space-y-2">
			<Label>{m.jobs_cron_examples()}</Label>
			<div class="grid grid-cols-2 gap-3 pt-2">
				{#each cronExamples as example (example.value)}
					<Button
						variant="outline"
						onclick={() => useCronExample(example.value)}
						class="min-h-12 items-start justify-start px-3 py-2 whitespace-normal"
					>
						<div class="text-left">
							<div class="text-xs leading-4 font-medium">{example.label}</div>
							<div class="text-muted-foreground font-mono text-xs leading-4">{example.value}</div>
						</div>
					</Button>
				{/each}
			</div>
		</div>
	</div>

	{#snippet footer()}
		<Button variant="outline" onclick={() => (open = false)} disabled={isLoading}>
			{m.common_cancel()}
		</Button>
		<Button onclick={save} disabled={isLoading}>
			{isLoading ? m.common_saving() : m.common_save()}
		</Button>
	{/snippet}
</ResponsiveDialog>
