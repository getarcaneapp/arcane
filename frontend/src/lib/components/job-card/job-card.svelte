<script lang="ts">
	import { StartIcon, EditIcon, ClockIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Spinner } from '$lib/components/ui/spinner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import { formatDistanceToNow } from 'date-fns';
	import type { Snippet } from 'svelte';
	import type { JobStatus } from '$lib/types/job-schedule.type';
	import JobScheduleDialog from './job-schedule-dialog.svelte';

	let {
		job,
		isAgent = false,
		onScheduleUpdate,
		children,
		enabledOverride,
		headerAccessory
	}: {
		job: JobStatus;
		isAgent?: boolean;
		onScheduleUpdate?: () => void;
		children?: Snippet;
		enabledOverride?: boolean;
		headerAccessory?: Snippet;
	} = $props();

	let isRunning = $state(false);
	let showScheduleDialog = $state(false);

	const nextRunText = $derived.by(() => {
		if (!job.nextRun) return null;
		const nextRunDate = new Date(job.nextRun);
		const relative = formatDistanceToNow(nextRunDate, { addSuffix: true });
		const absolute = nextRunDate.toLocaleString();
		return `${relative} (${absolute})`;
	});

	const isEnabled = $derived.by(() => enabledOverride ?? job.enabled);

	const canRun = $derived(isEnabled && job.canRunManually && !isRunning && !(isAgent && job.managerOnly));

	async function runJobNow() {
		if (!canRun) return;

		isRunning = true;
		const result = await tryCatch(jobScheduleService.runJob(job.id));
		isRunning = false;

		if (result.error || !result.data?.success) {
			return;
		}
	}

	function openScheduleDialog() {
		showScheduleDialog = true;
	}

	function handleScheduleUpdated() {
		showScheduleDialog = false;
		onScheduleUpdate?.();
	}
</script>

<Card.Root class="bg-background/60 h-full rounded-xl border border-white/10 shadow-none backdrop-blur-md">
	<Card.Header class="flex flex-row items-center justify-between space-y-0 px-4 py-4">
		<div class="flex flex-col gap-1.5">
			<div class="flex items-center gap-2">
				<Card.Title class="text-sm leading-none font-medium">{job.name}</Card.Title>
				{#if job.isContinuous}
					<Badge variant="outline" class="h-4.5 border-white/10 px-1.5 text-[0.6rem] font-medium">{m.jobs_continuous()}</Badge>
				{/if}
			</div>
			<Card.Description class="text-muted-foreground/80 line-clamp-1 text-xs">{job.description}</Card.Description>
		</div>
		<div class="flex items-center gap-2">
			{#if headerAccessory}
				{@render headerAccessory()}
			{/if}
			{#if isEnabled && !job.isContinuous && job.settingsKey}
				<Button
					variant="ghost"
					size="icon"
					onclick={openScheduleDialog}
					disabled={isAgent && job.managerOnly}
					class="size-7 opacity-75 hover:bg-white/5 hover:opacity-100"
				>
					<EditIcon class="size-3.5" />
				</Button>
			{/if}
			{#if isEnabled && job.canRunManually}
				<Button
					variant="outline"
					size="sm"
					onclick={runJobNow}
					disabled={!canRun}
					class="h-7 border-white/10 bg-transparent px-2.5 text-xs font-medium shadow-none hover:bg-white/5"
				>
					{#if isRunning}
						<Spinner class="mr-1.5 size-3" />
						{m.common_running()}
					{:else}
						<StartIcon class="mr-1.5 size-3.5" />
						{m.jobs_run_now()}
					{/if}
				</Button>
			{/if}
		</div>
	</Card.Header>
	<Card.Content class="space-y-2 px-4 pt-0 pb-4">
		{#if isEnabled}
			<div class="text-muted-foreground grid gap-2 text-xs">
				{#if !job.isContinuous}
					<div class="flex items-center gap-2">
						<ClockIcon class="h-3.5 w-3.5" />
						<span>{m.jobs_schedule()}:</span>
						<code class="bg-muted rounded px-2 py-0.5 text-[0.65rem]">{job.schedule}</code>
					</div>
					{#if nextRunText}
						<div>
							{m.jobs_next_run()}: {nextRunText}
						</div>
					{/if}
				{:else}
					<div>{m.jobs_continuous()}</div>
				{/if}
			</div>
		{/if}

		{#if children}
			{@render children()}
		{/if}
	</Card.Content>
</Card.Root>

{#if showScheduleDialog}
	<JobScheduleDialog {job} bind:open={showScheduleDialog} onUpdate={handleScheduleUpdated} />
{/if}
