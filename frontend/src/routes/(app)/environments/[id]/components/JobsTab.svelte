<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';
	import { jobScheduleService } from '$lib/services/job-schedule-service';
	import { containerService } from '$lib/services/container-service';
	import { tryCatch } from '$lib/utils/api';
	import JobCard from '$lib/components/job-card/job-card.svelte';
	import { Spinner } from '$lib/components/ui/spinner';
	import { m } from '$lib/paraglide/messages';
	import * as Card from '$lib/components/ui/card';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Input } from '$lib/components/ui/input';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as ScrollArea from '$lib/components/ui/scroll-area';
	import { JobsIcon } from '$lib/icons';
	import type { JobStatus, JobPrerequisite } from '$lib/types/settings';
	import type { ContainerSummaryDto } from '$lib/types/docker';
	import type { JobsTabProps } from './tab-props';

	let { formInputs, environmentId }: JobsTabProps = $props();

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

	function parseExcludedContainerSet(value: string | undefined) {
		return new SvelteSet(
			(value || '')
				.split(',')
				.map((s: string) => normalizeContainerName(s.trim()))
				.filter(Boolean)
		);
	}

	function toggleExcludedContainerValue(current: SvelteSet<string>, containerName: string): string {
		const normalizedName = normalizeContainerName(containerName);
		const newSet = new SvelteSet(current);
		if (newSet.has(normalizedName)) {
			newSet.delete(normalizedName);
		} else {
			newSet.add(normalizedName);
		}

		return Array.from(newSet).join(',');
	}

	const excludedContainers = $derived.by(() => {
		return parseExcludedContainerSet($formInputs.autoUpdateExcludedContainers?.value);
	});

	function resolveSettingsUrl(_job: JobStatus, prereq: JobPrerequisite): string | undefined {
		if (!prereq.settingsUrl) return undefined;
		if (!environmentId) return prereq.settingsUrl;

		const envBase = `/environments/${environmentId}`;
		switch (prereq.settingKey) {
			case 'pollingEnabled':
			case 'autoUpdate':
				return `${envBase}?tab=docker`;
			case 'scheduledPruneEnabled':
				return `${envBase}?tab=jobs`;
			case 'vulnerabilityScanEnabled':
				return undefined;
			case 'autoHealEnabled':
				return `${envBase}?tab=jobs`;
			default:
				return prereq.settingsUrl;
		}
	}

	function loadJobs() {
		refreshSignal++;
	}

	function toggleContainerExclusion(containerName: string) {
		if ($formInputs.autoUpdateExcludedContainers) {
			$formInputs.autoUpdateExcludedContainers.value = toggleExcludedContainerValue(excludedContainers, containerName);
		}
	}

	const autoHealExcludedContainers = $derived.by(() => {
		return parseExcludedContainerSet($formInputs.autoHealExcludedContainers?.value);
	});

	function toggleAutoHealContainerExclusion(containerName: string) {
		if ($formInputs.autoHealExcludedContainers) {
			$formInputs.autoHealExcludedContainers.value = toggleExcludedContainerValue(autoHealExcludedContainers, containerName);
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
		{ id: 'maintenance', label: m.maintenance() },
		{ id: 'security', label: m.security() },
		{ id: 'updates', label: m.updates() },
		{ id: 'sync', label: m.resource_sync_cap() },
		{ id: 'telemetry', label: m.jobs_telemetry_heading() }
	];

	const hiddenJobIds = new Set(['analytics-heartbeat', 'filesystem-watcher']);

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
</script>

{#snippet ContainerExclusionList(config: {
	term: string;
	mapItem: (container: ContainerSummaryDto) => {
		value: string;
		label: string;
		disabled?: boolean;
		hint?: string;
		selected: boolean;
	};
	idPrefix: string;
	onToggle: (containerName: string) => void;
})}
	<ScrollArea.Root class="h-64 w-full rounded-md border p-2">
		<div class="space-y-2">
			{#await containersPromise}
				<div class="flex items-center justify-center p-4">
					<Spinner class="size-4" />
				</div>
			{:then containers}
				{@const allItems = containers.map(config.mapItem)}
				{@const filteredItems = config.term
					? allItems.filter((item) => item.label.toLowerCase().includes(config.term.toLowerCase()))
					: allItems}

				{#if filteredItems.length === 0}
					<p class="py-4 text-center text-sm text-muted-foreground">
						{m.common_no_results_found()}
					</p>
				{:else}
					{#each filteredItems as container (container.value)}
						<div class="flex items-center space-x-2">
							<Checkbox
								id="{config.idPrefix}{container.value}"
								checked={container.selected}
								disabled={container.disabled}
								onCheckedChange={() => config.onToggle(container.value)}
							/>
							<Label
								for="{config.idPrefix}{container.value}"
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
				<div class="p-2 text-sm text-destructive">
					{(error instanceof Error ? error.message : '') || 'Failed to load containers'}
				</div>
			{/await}
		</div>
	</ScrollArea.Root>
{/snippet}

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
									<h3 class="text-sm font-semibold tracking-tight text-muted-foreground uppercase">
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
													{:else if job.id === 'auto-heal'}
														<Switch bind:checked={$formInputs.autoHealEnabled.value} />
													{/if}
												{/snippet}

												{#if job.id === 'auto-update' && $formInputs.autoUpdate.value}
													<div class="space-y-3 border-t border-border/20 pt-3">
														<div class="space-y-1">
															<Label class="text-sm font-medium">
																{m.excluded_containers()}
																{#await containersPromise then containers}
																	<span class="ml-1 font-normal text-muted-foreground">
																		({containers.filter((c) => excludedContainers.has(getContainerName(c))).length})
																	</span>
																{/await}
															</Label>
															<p class="text-xs text-muted-foreground">{m.auto_update_exclude_description()}</p>
														</div>

														<div class="space-y-2">
															<Input type="search" placeholder={m.jobs_search_containers()} class="h-8" bind:value={searchTerm} />
															{@render ContainerExclusionList({
																term: searchTerm,
																mapItem: mapContainerToItem,
																idPrefix: 'container-',
																onToggle: toggleContainerExclusion
															})}
														</div>
													</div>
												{/if}

												{#if job.id === 'auto-heal' && $formInputs.autoHealEnabled.value}
													<div class="space-y-3 border-t border-border/20 pt-3">
														<div class="grid gap-3 sm:grid-cols-2">
															<div class="space-y-1">
																<Label for="auto-heal-max-restarts" class="text-sm font-medium"
																	>{m.auto_heal_max_restarts_label()}</Label
																>
																<p class="text-xs text-muted-foreground">{m.auto_heal_max_restarts_description()}</p>
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
																<p class="text-xs text-muted-foreground">{m.auto_heal_restart_window_description()}</p>
																<Input
																	id="auto-heal-restart-window"
																	type="number"
																	min="1"
																	class="h-8 w-full"
																	bind:value={$formInputs.autoHealRestartWindow.value}
																/>
															</div>
														</div>

														<div class="space-y-1">
															<Label class="text-sm font-medium">
																{m.excluded_containers()}
																{#await containersPromise then containers}
																	<span class="ml-1 font-normal text-muted-foreground">
																		({containers.filter((c) => autoHealExcludedContainers.has(getContainerName(c))).length})
																	</span>
																{/await}
															</Label>
															<p class="text-xs text-muted-foreground">{m.auto_heal_exclude_description()}</p>
														</div>

														<div class="space-y-2">
															<Input
																type="search"
																placeholder={m.jobs_search_containers()}
																class="h-8"
																bind:value={autoHealSearchTerm}
															/>
															{@render ContainerExclusionList({
																term: autoHealSearchTerm,
																mapItem: mapContainerToAutoHealItem,
																idPrefix: 'auto-heal-container-',
																onToggle: toggleAutoHealContainerExclusion
															})}
														</div>
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
				<div class="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-destructive">
					{(error instanceof Error ? error.message : '') || String(error)}
				</div>
			{/await}
		</Card.Content>
	</Card.Root>
</div>
