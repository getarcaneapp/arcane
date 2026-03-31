<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import { containerService } from '$lib/services/container-service';
	import { tryCatch } from '$lib/utils/try-catch';
	import JobCard from '$lib/components/job-card/job-card.svelte';
	import ContainerSelectionPanel from './ContainerSelectionPanel.svelte';
	import { Spinner } from '$lib/components/ui/spinner';
	import { m } from '$lib/paraglide/messages';
	import * as Card from '$lib/components/ui/card';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Input } from '$lib/components/ui/input';
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
					settingsUrl: resolveSettingsUrl(prereq)
				}))
			}))
		};
	});

	const containersPromise = $derived.by(async () => {
		if (!environmentId) return [];
		if (!$formInputs.autoUpdate.value && !$formInputs.autoHealEnabled.value) return [];
		const result = await tryCatch(
			containerService.getContainersForEnvironment(environmentId, { pagination: { page: 1, limit: 100 } })
		);
		if (result.error) throw result.error;
		return result.data.data;
	});

	let searchTerm = $state('');
	let autoHealSearchTerm = $state('');

	const excludedContainers = $derived.by(() => {
		const savedValue = $formInputs.autoUpdateExcludedContainers?.value || '';
		return new SvelteSet(
			savedValue
				.split(',')
				.map((s: string) => normalizeContainerName(s.trim()))
				.filter(Boolean)
		);
	});

	function resolveSettingsUrl(prereq: JobPrerequisite): string | undefined {
		if (!prereq.settingsUrl) return undefined;
		if (!environmentId) return prereq.settingsUrl;

		const envBase = `/settings/environments?environment=${environmentId}`;
		switch (prereq.settingKey) {
			case 'pollingEnabled':
			case 'autoUpdate':
				return `${envBase}&tab=docker`;
			case 'gitopsSyncEnabled':
				return `/environments/${environmentId}/gitops`;
			case 'scheduledPruneEnabled':
				return undefined;
			case 'vulnerabilityScanEnabled':
				return undefined;
			case 'autoHealEnabled':
				return `${envBase}&tab=jobs`;
			default:
				return prereq.settingsUrl;
		}
	}

	function loadJobs() {
		refreshSignal++;
	}

	function toggleContainerExclusion(containerName: string) {
		const normalizedName = normalizeContainerName(containerName);
		const newSet = new SvelteSet(excludedContainers);
		if (newSet.has(normalizedName)) {
			newSet.delete(normalizedName);
		} else {
			newSet.add(normalizedName);
		}

		if ($formInputs.autoUpdateExcludedContainers) {
			$formInputs.autoUpdateExcludedContainers.value = Array.from(newSet).join(',');
		}
	}

	const autoHealExcludedContainers = $derived.by(() => {
		const savedValue = $formInputs.autoHealExcludedContainers?.value || '';
		return new SvelteSet(
			savedValue
				.split(',')
				.map((s: string) => normalizeContainerName(s.trim()))
				.filter(Boolean)
		);
	});

	function toggleAutoHealContainerExclusion(containerName: string) {
		const normalizedName = normalizeContainerName(containerName);
		const newSet = new SvelteSet(autoHealExcludedContainers);
		if (newSet.has(normalizedName)) {
			newSet.delete(normalizedName);
		} else {
			newSet.add(normalizedName);
		}

		if ($formInputs.autoHealExcludedContainers) {
			$formInputs.autoHealExcludedContainers.value = Array.from(newSet).join(',');
		}
	}

	function mapContainerToAutoHealItem(container: ContainerSummaryDto) {
		const name = getContainerName(container);
		return {
			value: name,
			label: name,
			selected: autoHealExcludedContainers.has(name)
		};
	}

	const categories = [
		{ id: 'monitoring', label: m.jobs_monitoring_heading() },
		{ id: 'maintenance', label: m.jobs_maintenance_heading() },
		{ id: 'security', label: m.jobs_security_heading() },
		{ id: 'updates', label: m.jobs_updates_heading() },
		{ id: 'sync', label: m.jobs_sync_heading() },
		{ id: 'telemetry', label: m.jobs_telemetry_heading() }
	];

	type ScheduledPruneField =
		| 'scheduledPruneContainers'
		| 'scheduledPruneImages'
		| 'scheduledPruneVolumes'
		| 'scheduledPruneNetworks'
		| 'scheduledPruneBuildCache';

	const scheduledPruneOptions = $derived.by(
		(): Array<{ field: ScheduledPruneField; label: string; description: string }> => [
			{
				field: 'scheduledPruneContainers',
				label: m.scheduled_prune_containers_label(),
				description: m.scheduled_prune_containers_description()
			},
			{
				field: 'scheduledPruneImages',
				label: m.scheduled_prune_images_label(),
				description: m.scheduled_prune_images_description()
			},
			{
				field: 'scheduledPruneVolumes',
				label: m.scheduled_prune_volumes_label(),
				description: m.scheduled_prune_volumes_description()
			},
			{
				field: 'scheduledPruneNetworks',
				label: m.scheduled_prune_networks_label(),
				description: m.scheduled_prune_networks_description()
			},
			{
				field: 'scheduledPruneBuildCache',
				label: m.scheduled_prune_build_cache_label(),
				description: m.scheduled_prune_build_cache_description()
			}
		]
	);

	const hiddenJobIds = new Set(['analytics-heartbeat', 'gitops-sync', 'filesystem-watcher']);

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
			case 'auto-heal':
				return $formInputs.autoHealEnabled.value;
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

	function setScheduledPruneField(field: ScheduledPruneField, checked: boolean) {
		$formInputs[field].value = checked;
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
		<Card.Content class="space-y-0 lg:p-6 lg:pt-0">
			{#await jobsPromise}
				<div class="flex h-32 items-center justify-center">
					<Spinner class="size-8" />
				</div>
			{:then jobsResponse}
				{#if jobsResponse}
					<div class="space-y-12">
						{#each categories as category (category.id)}
							{@const categoryJobs = getJobsByCategory(category.id, jobsResponse.jobs)}
							{#if categoryJobs.length > 0}
								<div class="space-y-4">
									<h3 class="text-muted-foreground ml-1 text-xs font-semibold tracking-wider uppercase">
										{category.label}
									</h3>
									<div class="grid gap-6 sm:grid-cols-2 lg:grid-cols-2">
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
													{:else if job.id === 'auto-heal'}
														<Switch bind:checked={$formInputs.autoHealEnabled.value} />
													{/if}
												{/snippet}

												{#if job.id === 'auto-update' && $formInputs.autoUpdate.value}
													<div class="border-border/20 space-y-3 border-t pt-3">
														<ContainerSelectionPanel
															title={m.auto_update_excluded_containers()}
															description={m.auto_update_exclude_description()}
															{containersPromise}
															{mapContainerToItem}
															toggleItem={toggleContainerExclusion}
															inputIdPrefix="container"
															bind:searchTerm
														/>
													</div>
												{/if}

												{#if job.id === 'auto-heal' && $formInputs.autoHealEnabled.value}
													<div class="border-border/20 space-y-3 border-t pt-3">
														<div class="grid gap-3 sm:grid-cols-2">
															<div class="space-y-1">
																<Label for="auto-heal-max-restarts" class="text-sm font-medium"
																	>{m.auto_heal_max_restarts_label()}</Label
																>
																<p class="text-muted-foreground text-xs">{m.auto_heal_max_restarts_description()}</p>
																<Input
																	id="auto-heal-max-restarts"
																	type="number"
																	min="1"
																	class="h-8 w-full"
																	bind:value={$formInputs.autoHealMaxRestarts.value}
																/>
															</div>
															<div class="space-y-1">
																<Label for="auto-heal-restart-window" class="text-sm font-medium"
																	>{m.auto_heal_restart_window_label()}</Label
																>
																<p class="text-muted-foreground text-xs">{m.auto_heal_restart_window_description()}</p>
																<Input
																	id="auto-heal-restart-window"
																	type="number"
																	min="1"
																	class="h-8 w-full"
																	bind:value={$formInputs.autoHealRestartWindow.value}
																/>
															</div>
														</div>

														<ContainerSelectionPanel
															title={m.auto_heal_excluded_containers()}
															description={m.auto_heal_exclude_description()}
															{containersPromise}
															mapContainerToItem={mapContainerToAutoHealItem}
															toggleItem={toggleAutoHealContainerExclusion}
															inputIdPrefix="auto-heal-container"
															bind:searchTerm={autoHealSearchTerm}
														/>
													</div>
												{/if}

												{#if job.id === 'scheduled-prune' && $formInputs.scheduledPruneEnabled.value}
													<div class="border-border/20 space-y-4 border-t pt-3">
														<div class="grid gap-3 sm:grid-cols-2">
															{#each scheduledPruneOptions as option (option.field)}
																<div class="bg-muted/20 ring-border/20 flex items-start justify-between rounded-lg p-3 ring-1">
																	<div class="space-y-0.5">
																		<Label class="text-sm font-medium">{option.label}</Label>
																		<p class="text-muted-foreground text-xs">{option.description}</p>
																	</div>
																	<Switch
																		checked={$formInputs[option.field].value}
																		onCheckedChange={(checked) => setScheduledPruneField(option.field, checked)}
																	/>
																</div>
															{/each}
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
