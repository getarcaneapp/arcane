<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ActionMenuItem from '#lib/components/arcane-table/cells/action-menu-item.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { ClockIcon, JobsIcon, PlayIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectService } from '#lib/services/project-service.js';
	import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte.js';
	import type { ProjectJob } from '#lib/types/project.js';
	import { cn } from '#lib/utils.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	interface Props {
		jobs: ProjectJob[];
		projectId: string;
		environmentId: string;
	}

	let { jobs, projectId, environmentId }: Props = $props();

	const canRunJob = $derived(hasPermission('projects:jobs:run', environmentId));

	let runningJob = $state('');

	function runDisabledReason(job: ProjectJob): string | undefined {
		if (job.disabled) return m.disabled_by_inactive_profiles();
		if (!job.manualAllowed) return m.manual_runs_not_allowed();
		return undefined;
	}

	async function runJob(job: ProjectJob) {
		runningJob = job.name;
		operationWatchStore.start(`${m.run()} — ${job.name}`);
		const result = await tryCatch(
			projectService.runProjectJob(projectId, job.name, (frame) => operationWatchStore.onLine(frame), environmentId)
		);
		runningJob = '';
		if (result.error === null) return;
		const message = result.error instanceof Error ? result.error.message : m.job_run_failed();
		if (operationWatchStore.open) {
			operationWatchStore.fail(message);
		} else {
			toast.error(m.job_run_failed(), { description: message });
		}
	}
</script>

<div class="flex min-h-0 flex-col">
	<div class="flex h-10 shrink-0 items-center gap-2 px-2">
		<JobsIcon class="size-4 text-muted-foreground" />
		<span class="text-sm font-medium">{m.jobs()}</span>
	</div>
	<div class="min-h-0 flex-1 space-y-0.5 overflow-y-auto">
		{#each jobs as job (job.name)}
			{@const reason = runDisabledReason(job)}
			<div class="group rounded-lg px-2 py-1.5 transition-colors hover:bg-foreground/4 has-data-[state=open]:bg-foreground/7">
				<div class="flex items-center gap-2">
					<JobsIcon class="size-4 shrink-0 text-muted-foreground" />
					<span class={cn('min-w-0 flex-1 truncate text-sm', job.disabled && 'text-muted-foreground')} title={job.name}>
						{job.name}
					</span>
					{#if job.disabled}
						<Badge variant="secondary">{m.common_disabled()}</Badge>
					{/if}
					{#if canRunJob}
						<RowActionsMenu
							name={job.name}
							triggerClass="size-6 shrink-0 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100"
						>
							<ActionMenuItem
								icon={PlayIcon}
								label={m.run()}
								onclick={() => runJob(job)}
								loading={runningJob === job.name}
								disabled={reason !== undefined || runningJob !== ''}
								title={reason}
							/>
						</RowActionsMenu>
					{/if}
				</div>
				<div class="mt-1 flex min-w-0 flex-col gap-0.5 pl-6 text-xs text-muted-foreground">
					{#if job.image}
						<span class="truncate font-mono" title={job.image}>{job.image}</span>
					{/if}
					{#each job.schedules as schedule (`${schedule.cron}|${schedule.timezone ?? ''}`)}
						<span class="flex min-w-0 items-center gap-1">
							<ClockIcon class="size-3 shrink-0" />
							<span class="truncate font-mono">{schedule.cron}</span>
							{#if schedule.timezone}
								<span class="truncate">{schedule.timezone}</span>
							{/if}
						</span>
					{:else}
						<span>{m.manual_only()}</span>
					{/each}
					{#if reason}
						<span>{reason}</span>
					{/if}
				</div>
			</div>
		{/each}
	</div>
</div>
