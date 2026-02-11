<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import { containerService } from '$lib/services/container-service';
	import { tryCatch } from '$lib/utils/try-catch';
	import JobCard from '$lib/components/job-card/job-card.svelte';
	import { Spinner } from '$lib/components/ui/spinner';
	import { m } from '$lib/paraglide/messages';
	import * as Card from '$lib/components/ui/card';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Input } from '$lib/components/ui/input';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as ScrollArea from '$lib/components/ui/scroll-area';
	import { JobsIcon, AlertIcon } from '$lib/icons';
	import type { JobStatus, JobPrerequisite } from '$lib/types/job-schedule.type';
	import type { ContainerSummaryDto } from '$lib/types/container.type';

	let { formInputs, environmentId } = $props();

	let refreshSignal = $state(0);

	const jobsPromise = $derived.by(async () => {
		refreshSignal; // trigger dependency
		if (!environmentId) return null;

		const result = await tryCatch(jobScheduleService.listJobs(environmentId));

		if (result.error) {
			throw result.error;
		}

		return {
			...result.data,
			jobs: result.data.jobs.map((job) => ({
				...job,
				prerequisites: job.prerequisites.map((prereq) => ({
					...prereq,
					settingsUrl: resolveSettingsUrl(job, prereq)
				}))
			}))
		};
	});

	const containersPromise = $derived.by(async () => {
		if (!$formInputs.autoUpdate.value) return [];
		const result = await tryCatch(containerService.getContainers({ pagination: { page: 1, limit: 100 } }));
		if (result.error) throw result.error;
		return result.data.data;
	});

	const excludedContainers = new SvelteSet<string>();
	let searchTerm = $state('');



	$effect(() => {
		const savedValue = $formInputs.autoUpdateExcludedContainers?.value || '';
		const names = savedValue
			.split(',')
			.map((s: string) => normalizeContainerName(s.trim()))
			.filter(Boolean);

		// Synchronize the SvelteSet with the form input value
		// Only update if there are actual changes to avoid unnecessary reactivity
		const currentNames = Array.from(excludedContainers);
		if (names.length !== currentNames.length || names.some((n: string) => !excludedContainers.has(n))) {
			excludedContainers.clear();
			names.forEach((n: string) => excludedContainers.add(n));
		}
	});

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
			case 'vulnerabilityScanEnabled':
				return undefined;
			default:
				return prereq.settingsUrl;
		}
	}

	function loadJobs() {
		refreshSignal++;
	}

	function toggleContainerExclusion(containerName: string) {
		const normalizedName = normalizeContainerName(containerName);
		if (excludedContainers.has(normalizedName)) {
			excludedContainers.delete(normalizedName);
		} else {
			excludedContainers.add(normalizedName);
		}

		const newValue = Array.from(excludedContainers).join(',');
		if ($formInputs.autoUpdateExcludedContainers) {
			$formInputs.autoUpdateExcludedContainers.value = newValue;
		}
	}

	const categories = [
		{ id: 'monitoring', label: m.jobs_monitoring_heading() },
		{ id: 'maintenance', label: m.jobs_maintenance_heading() },
		{ id: 'security', label: m.jobs_security_heading() },
		{ id: 'updates', label: m.jobs_updates_heading() },
		{ id: 'sync', label: m.jobs_sync_heading() },
		{ id: 'telemetry', label: m.jobs_telemetry_heading() }
	];

	const hiddenJobIds = new Set(['gitops-sync', 'filesystem-watcher']);

	function getJobsByCategory(categoryId: string, jobs: JobStatus[]): JobStatus[] {
		return jobs.filter((j) => {
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
			case 'vulnerability-scan':
				return $formInputs.vulnerabilityScanEnabled.value;
			default:
				return undefined;
			}
	}

	function getContainerName(c: ContainerSummaryDto): string {
		const rawName = c.names[0] || c.id.substring(0, 12);
		return normalizeContainerName(rawName);
	}

	function normalizeContainerName(name: string): string {
		return name.replace(/^\/+/, '');
	}

	function isContainerLabelExcluded(container: ContainerSummaryDto): boolean {
		const labels = container.labels || {};
		for (const [k, v] of Object.entries(labels)) {
			if (k.toLowerCase() === 'com.getarcaneapp.arcane.updater') {
				return ['false', '0', 'no', 'off'].includes(v.trim().toLowerCase());
			}
		}
		return false;
	}

	function mapContainerToItem(container: ContainerSummaryDto) {
		const name = getContainerName(container);
		const labelExcluded = isContainerLabelExcluded(container);
		return {
			value: name,
			label: name,
			disabled: labelExcluded,
			hint: labelExcluded ? '(Label)' : undefined,
			selected: excludedContainers.has(name)
		};
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
			{#await jobsPromise}
				<div class="flex h-32 items-center justify-center">
					<Spinner class="size-8" />
				</div>
			{:then jobsResponse}
				{#if jobsResponse}
					<div class="space-y-8">
						{#each categories as category (category.id)}
							{@const categoryJobs = getJobsByCategory(category.id, jobsResponse.jobs)}
							{#if categoryJobs.length > 0}
								<div class="space-y-4">
									<h3 class="text-muted-foreground text-sm font-semibold tracking-tight uppercase">
										{category.label}
									</h3>
									<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-2">
										{#each categoryJobs as job (job.id)}
											<JobCard
												{job}
												{environmentId}
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
													{:else if job.id === 'vulnerability-scan'}
														<Switch bind:checked={$formInputs.vulnerabilityScanEnabled.value} />
													{/if}
												{/snippet}

													{#if job.id === 'auto-update' && $formInputs.autoUpdate.value}
														<div class="border-border/20 space-y-3 border-t pt-3">
															<div class="space-y-1">
																<Label class="text-sm font-medium">
																	{m.auto_update_excluded_containers()}
																	{#await containersPromise then containers}
																		<span class="text-muted-foreground ml-1 font-normal">
																			({containers.filter((c) => excludedContainers.has(getContainerName(c))).length})
																		</span>
																	{/await}
																</Label>
																<p class="text-muted-foreground text-xs">{m.auto_update_exclude_description()}</p>
															</div>

															<div class="rounded-md border p-2">
																<Input
																	type="search"
																	placeholder="Search containers..."
																	class="mb-2 h-8"
																	bind:value={searchTerm}
																/>
																<ScrollArea.Root class="h-64 w-full rounded-md border p-2">
																	<div class="space-y-2">
																		{#await containersPromise}
																			<div class="flex items-center justify-center p-4">
																				<Spinner class="size-4" />
																			</div>
																		{:then containers}
																			{@const allItems = containers.map(mapContainerToItem)}
																			{@const filteredItems = searchTerm 
																				? allItems.filter(item => item.label.toLowerCase().includes(searchTerm.toLowerCase())) 
																				: allItems}
																			
																			{#if filteredItems.length === 0}
																				<p class="text-muted-foreground text-center text-sm py-4">
																					{m.common_no_results_found()}
																				</p>
																			{:else}
																				{#each filteredItems as container (container.value)}
																					<div class="flex items-center space-x-2">
																						<Checkbox
																							id="container-{container.value}"
																							checked={container.selected}
																							disabled={container.disabled}
																							onCheckedChange={() => toggleContainerExclusion(container.value)}
																						/>
																						<Label
																							for="container-{container.value}"
																							class="text-sm font-normal {container.disabled ? 'text-muted-foreground' : ''}"
																						>
																							{container.label}
																							{#if container.hint}
																								<span class="ml-1 text-xs opacity-70">{container.hint}</span>
																							{/if}
																						</Label>
																					</div>
																				{/each}
																			{/if}
																		{:catch error}
																			<div class="text-destructive text-sm p-2">
																				{error.message || 'Failed to load containers'}
																			</div>
																		{/await}
																	</div>
																</ScrollArea.Root>
															</div>
														</div>
													{/if}

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
			{:catch error}
				<div class="border-destructive/50 bg-destructive/10 text-destructive rounded-lg border p-4">
					{error.message || error}
				</div>
			{/await}
		</Card.Content>
	</Card.Root>
</div>
