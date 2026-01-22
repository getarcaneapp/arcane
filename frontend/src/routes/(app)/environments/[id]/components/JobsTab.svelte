<script lang="ts">
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import { tryCatch } from '$lib/utils/try-catch';
	import JobCard from '$lib/components/job-card/job-card.svelte';
	import { Spinner } from '$lib/components/ui/spinner';
	import { m } from '$lib/paraglide/messages';
	import * as Card from '$lib/components/ui/card';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { JobsIcon, AlertIcon } from '$lib/icons';
	import type { JobListResponse, JobStatus, JobPrerequisite } from '$lib/types/job-schedule.type';

	let { formInputs, environmentId } = $props();

	let jobsResponse = $state<JobListResponse | null>(null);
	let isLoading = $state(true);
	let error = $state<string | null>(null);

	function resolveSettingsUrl(job: JobStatus, prereq: JobPrerequisite): string | undefined {
		if (!prereq.settingsUrl) return undefined;
		if (!environmentId) return prereq.settingsUrl;

		const envBase = `/environments/${environmentId}`;
		switch (prereq.settingKey) {
			case 'pollingEnabled':
			case 'autoUpdate':
				return `${envBase}?tab=docker`;
			case 'gitopsSyncEnabled':
				return `${envBase}?tab=gitops`;
			case 'scheduledPruneEnabled':
				return undefined;
			default:
				return prereq.settingsUrl;
		}
	}

	async function loadJobs() {
		isLoading = true;
		const result = await tryCatch(jobScheduleService.listJobs());
		isLoading = false;

		if (result.error) {
			error = result.error.message;
			return;
		}

		jobsResponse = {
			...result.data,
			jobs: result.data.jobs.map((job) => ({
				...job,
				prerequisites: job.prerequisites.map((prereq) => ({
					...prereq,
					settingsUrl: resolveSettingsUrl(job, prereq)
				}))
			}))
		};
	}

	$effect(() => {
		if (environmentId) {
			loadJobs();
		}
	});

	const categories = [
		{ id: 'monitoring', label: m.jobs_monitoring_heading() },
		{ id: 'maintenance', label: m.jobs_maintenance_heading() },
		{ id: 'updates', label: m.jobs_updates_heading() },
		{ id: 'sync', label: m.jobs_sync_heading() },
		{ id: 'telemetry', label: m.jobs_telemetry_heading() }
	];

	const hiddenJobIds = new Set(['gitops-sync', 'filesystem-watcher']);

	function getJobsByCategory(categoryId: string): JobStatus[] {
		if (!jobsResponse) return [];
		return jobsResponse.jobs.filter((j) => {
			if (hiddenJobIds.has(j.id)) return false;
			if (j.category !== categoryId) return false;
			// Only show manager-only jobs on the local environment (ID "0")
			if (j.managerOnly && environmentId !== '0') return false;
			return true;
		});
	}

	function getEnabledOverride(job: JobStatus): boolean | undefined {
		switch (job.id) {
			case 'scheduled-prune':
				return $formInputs.scheduledPruneEnabled.value;
			case 'auto-update':
				return $formInputs.autoUpdate.value;
			case 'image-polling':
				return $formInputs.pollingEnabled.value;
			default:
				return undefined;
		}
	}
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={JobsIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.jobs_title()}</h2>
				</Card.Title>
				<Card.Description>{m.jobs_environment_scope_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4 sm:p-6">
			{#if isLoading}
				<div class="flex h-32 items-center justify-center">
					<Spinner class="size-8" />
				</div>
			{:else if error}
				<div class="border-destructive/50 bg-destructive/10 text-destructive rounded-lg border p-4">
					{error}
				</div>
			{:else if jobsResponse}
				<div class="space-y-8">
					{#each categories as category (category.id)}
						{@const categoryJobs = getJobsByCategory(category.id)}
						{#if categoryJobs.length > 0}
							<div class="space-y-4">
								<h3 class="text-muted-foreground text-sm font-semibold tracking-tight uppercase">
									{category.label}
								</h3>
								<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-2">
									{#each categoryJobs as job (job.id)}
										<JobCard
											{job}
											isAgent={jobsResponse.isAgent}
											onScheduleUpdate={loadJobs}
											enabledOverride={getEnabledOverride(job)}
										>
											{#snippet headerAccessory()}
												{#if job.id === 'image-polling'}
													<Switch bind:checked={$formInputs.pollingEnabled.value} />
												{:else if job.id === 'auto-update'}
													<Switch bind:checked={$formInputs.autoUpdate.value} disabled={!$formInputs.pollingEnabled.value} />
												{:else if job.id === 'scheduled-prune'}
													<Switch bind:checked={$formInputs.scheduledPruneEnabled.value} />
												{/if}
											{/snippet}

											{#if job.id === 'scheduled-prune'}
												{#if $formInputs.scheduledPruneEnabled.value}
													<div class="border-border/20 space-y-4 border-t pt-3">
														<div class="grid gap-3 sm:grid-cols-2">
															<div class="bg-muted/20 ring-border/20 flex items-start justify-between rounded-lg p-3 ring-1">
																<div class="space-y-0.5">
																	<Label class="text-sm font-medium">{m.scheduled_prune_containers_label()}</Label>
																	<p class="text-muted-foreground text-xs">{m.scheduled_prune_containers_description()}</p>
																</div>
																<Switch bind:checked={$formInputs.scheduledPruneContainers.value} />
															</div>
															<div class="bg-muted/20 ring-border/20 flex items-start justify-between rounded-lg p-3 ring-1">
																<div class="space-y-0.5">
																	<Label class="text-sm font-medium">{m.scheduled_prune_images_label()}</Label>
																	<p class="text-muted-foreground text-xs">{m.scheduled_prune_images_description()}</p>
																</div>
																<Switch bind:checked={$formInputs.scheduledPruneImages.value} />
															</div>
															<div class="bg-muted/20 ring-border/20 flex items-start justify-between rounded-lg p-3 ring-1">
																<div class="space-y-0.5">
																	<Label class="text-sm font-medium">{m.scheduled_prune_volumes_label()}</Label>
																	<p class="text-muted-foreground text-xs">{m.scheduled_prune_volumes_description()}</p>
																</div>
																<Switch bind:checked={$formInputs.scheduledPruneVolumes.value} />
															</div>
															<div class="bg-muted/20 ring-border/20 flex items-start justify-between rounded-lg p-3 ring-1">
																<div class="space-y-0.5">
																	<Label class="text-sm font-medium">{m.scheduled_prune_networks_label()}</Label>
																	<p class="text-muted-foreground text-xs">{m.scheduled_prune_networks_description()}</p>
																</div>
																<Switch bind:checked={$formInputs.scheduledPruneNetworks.value} />
															</div>
															<div class="bg-muted/20 ring-border/20 flex items-start justify-between rounded-lg p-3 ring-1">
																<div class="space-y-0.5">
																	<Label class="text-sm font-medium">{m.scheduled_prune_build_cache_label()}</Label>
																	<p class="text-muted-foreground text-xs">{m.scheduled_prune_build_cache_description()}</p>
																</div>
																<Switch bind:checked={$formInputs.scheduledPruneBuildCache.value} />
															</div>
														</div>
														{#if $formInputs.scheduledPruneVolumes.value}
															<div
																class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-amber-900 dark:text-amber-200"
															>
																<AlertIcon class="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
																<div class="space-y-1 text-sm">
																	<p class="font-medium">{m.scheduled_prune_volumes_warning()}</p>
																</div>
															</div>
														{/if}
													</div>
												{/if}
											{/if}
										</JobCard>
									{/each}
								</div>
							</div>
						{/if}
					{/each}
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
