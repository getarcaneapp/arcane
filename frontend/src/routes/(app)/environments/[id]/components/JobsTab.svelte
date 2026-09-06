<script lang="ts">
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { SvelteSet } from 'svelte/reactivity';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { containerService } from '#lib/services/container-service.js';
	import { tryCatch } from '#lib/utils/api.js';
	import JobCard from '#lib/components/job-card/job-card.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as ScrollArea from '#lib/components/ui/scroll-area/index.js';
	import { AlertTriangleIcon, JobsIcon } from '#lib/icons/index.js';
	import type { JobStatus, JobPrerequisite } from '#lib/types/settings.js';
	import type { ContainerSummaryDto } from '#lib/types/docker.js';
	import type { JobsTabProps } from './tab-props';
	import {
		isAutoUpdateLabelDisabled,
		normalizeContainerName,
		parseExcludedContainerSet
	} from '#lib/utils/container-auto-update.js';

	let { formInputs, environmentId }: JobsTabProps = $props();

	const jobsQuery = createQuery(() => ({
		queryKey: queryKeys.jobs.list(environmentId),
		queryFn: async () => {
			const response = await jobScheduleService.listJobs(environmentId);
			return {
				...response,
				jobs: response.jobs.map((job) => ({
					...job,
					prerequisites: (job.prerequisites ?? []).map((prereq) => ({ ...prereq, settingsUrl: resolveSettingsUrl(job, prereq) }))
				}))
			};
		},
		enabled: !!environmentId,
		refetchInterval: 5000
	}));
	const jobsResponse = $derived(jobsQuery.data);
	const capabilityUnknown = $derived(
		!!jobsResponse?.offline && (!jobsResponse.observedAt || jobsResponse.observedAt.startsWith('0001-'))
	);

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

	function toggleExcludedContainerValue(current: ReadonlySet<string>, containerName: string): string {
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
				return `${envBase}?tab=jobs`;
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
		void jobsQuery.refetch();
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
		{ id: 'updates', label: m.updates() },
		{ id: 'monitoring', label: m.jobs_monitoring_heading() },
		{ id: 'maintenance', label: m.maintenance() },
		{ id: 'security', label: m.security() },
		{ id: 'sync', label: m.resource_sync_cap() },
		{ id: 'telemetry', label: m.jobs_telemetry_heading() }
	];

	function getJobsByCategory(categoryId: string, jobs: JobStatus[]): JobStatus[] {
		return jobs.filter((j) => {
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

	function mapContainerToItem(container: ContainerSummaryDto) {
		const name = getContainerName(container);
		const labelExcluded = isAutoUpdateLabelDisabled(container.labels);
		return {
			value: name,
			label: name,
			disabled: labelExcluded,
			hint: labelExcluded ? m.jobs_container_label_excluded() : undefined,
			selected: excludedContainers.has(name)
		};
	}
</script>

{#snippet jobCategory(label: string, categoryJobs: JobStatus[], isAgent: boolean, durableRuns: boolean)}
	{#if categoryJobs.length > 0}
		<div
			class="grid min-w-0 gap-x-6 gap-y-2 rounded-xl border border-border bg-transparent px-4 py-2 sm:px-5 lg:grid-cols-[7rem_minmax(0,1fr)]"
		>
			<h3 class="flex min-h-9 items-center text-sm font-semibold text-foreground lg:mt-5 lg:self-start">
				{label}
			</h3>
			<div class="min-w-0 divide-y divide-border/50">
				{#each categoryJobs as job (job.id)}
					{@render environmentJob(job, isAgent, durableRuns)}
				{/each}
			</div>
		</div>
	{/if}
{/snippet}

{#snippet autoUpdateSettings(job: JobStatus)}
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
{/snippet}

{#snippet imagePollingSettings(job: JobStatus)}
	{#if job.id === 'image-polling'}
		<div class="space-y-3 border-t border-border/20 pt-3">
			<div class="flex items-center justify-between gap-3">
				<div class="space-y-1">
					<Label class="text-sm font-medium">{m.jobs_image_event_watcher_label()}</Label>
					<p class="text-xs text-muted-foreground">{m.jobs_image_event_watcher_description()}</p>
				</div>
				<Switch id="image-event-watcher-enabled" bind:checked={$formInputs.imageEventWatcherEnabled.value} />
			</div>
			<Alert.Root variant="warning" class="py-2 [&>svg]:top-2">
				<AlertTriangleIcon class="size-4" />
				<Alert.Description class="text-xs">{m.jobs_image_event_watcher_warning()}</Alert.Description>
			</Alert.Root>
		</div>
	{/if}
{/snippet}

{#snippet autoHealSettings(job: JobStatus)}
	{#if job.id === 'auto-heal' && $formInputs.autoHealEnabled.value}
		<div class="space-y-3 border-t border-border/20 pt-3">
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="space-y-1">
					<Label for="auto-heal-max-restarts" class="text-sm font-medium">{m.auto_heal_max_restarts_label()}</Label>
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
					<Label for="auto-heal-restart-window" class="text-sm font-medium">{m.auto_heal_restart_window_label()}</Label>
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
				<Input type="search" placeholder={m.jobs_search_containers()} class="h-8" bind:value={autoHealSearchTerm} />
				{@render ContainerExclusionList({
					term: autoHealSearchTerm,
					mapItem: mapContainerToAutoHealItem,
					idPrefix: 'auto-heal-container-',
					onToggle: toggleAutoHealContainerExclusion
				})}
			</div>
		</div>
	{/if}
{/snippet}

{#snippet jobEnableControl(job: JobStatus)}
	{#if job.id === 'image-polling'}
		<Switch aria-label={job.name} bind:checked={$formInputs.pollingEnabled.value} />
	{:else if job.id === 'auto-update'}
		<Switch aria-label={job.name} bind:checked={$formInputs.autoUpdate.value} disabled={!$formInputs.pollingEnabled.value} />
	{:else if job.id === 'scheduled-prune'}
		<Switch aria-label={job.name} bind:checked={$formInputs.scheduledPruneEnabled.value} />
	{:else if job.id === 'vulnerability-scan'}
		<Switch aria-label={job.name} bind:checked={$formInputs.vulnerabilityScanEnabled.value} />
	{:else if job.id === 'auto-heal'}
		<Switch aria-label={job.name} bind:checked={$formInputs.autoHealEnabled.value} />
	{/if}
{/snippet}

{#snippet environmentJob(job: JobStatus, isAgent: boolean, durableRuns: boolean)}
	<JobCard
		{job}
		{environmentId}
		{isAgent}
		durableRuns={durableRuns || capabilityUnknown}
		onScheduleUpdate={loadJobs}
		enabledOverride={capabilityUnknown ? undefined : getEnabledOverride(job)}
		collapsibleSettings={job.id === 'image-polling' ||
			(job.id === 'auto-update' && $formInputs.autoUpdate.value) ||
			(job.id === 'auto-heal' && $formInputs.autoHealEnabled.value)}
	>
		{#snippet headerAccessory()}
			{@render jobEnableControl(job)}
		{/snippet}

		{@render autoUpdateSettings(job)}

		{@render imagePollingSettings(job)}

		{@render autoHealSettings(job)}
		{#if job.children?.length}
			<details class="mt-1">
				<summary
					class="w-fit cursor-pointer rounded-sm text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
					>{m.jobs_target_runs({ count: job.children.length })}</summary
				>
				<div class="mt-3 divide-y divide-border border-l border-border pl-4 sm:pl-6">
					{#each job.children ?? [] as child (child.id)}
						<JobCard
							job={child}
							{environmentId}
							{isAgent}
							durableRuns={durableRuns || capabilityUnknown}
							onScheduleUpdate={loadJobs}
						/>
					{/each}
				</div>
			</details>
		{/if}
	</JobCard>
{/snippet}

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
					{(error instanceof Error ? error.message : '') || m.jobs_containers_load_error()}
				</div>
			{/await}
		</div>
	</ScrollArea.Root>
{/snippet}

<section class="flex w-full min-w-0 flex-col gap-6">
	<header class="flex items-start gap-3">
		<JobsIcon class="mt-0.5 size-5 text-muted-foreground" />
		<div class="flex flex-col gap-1.5">
			<h2 class="text-lg font-semibold">{m.automations()}</h2>
			<p class="text-sm text-muted-foreground">{m.jobs_environment_scope_description()}</p>
		</div>
	</header>
	<div>
		{#if jobsQuery.isPending}
			<div class="flex h-32 items-center justify-center">
				<Spinner class="size-8" />
			</div>
		{:else}
			{#if jobsResponse}
				{#if capabilityUnknown}<p class="mb-4 text-sm text-muted-foreground">{m.jobs_capability_unknown()}</p>
				{:else if !jobsResponse.durableRuns}<p class="mb-4 text-sm text-muted-foreground">{m.jobs_upgrade_required()}</p>{/if}
				{#if jobsResponse.offline}<p class="mb-4 text-sm text-muted-foreground">
						{m.jobs_offline_status()}
						{#if !capabilityUnknown}{m.jobs_last_confirmed()}: {jobsResponse.observedAt}{/if}
					</p>{/if}
				<div class="grid items-start gap-x-12 gap-y-10 2xl:grid-cols-2">
					{#each categories as category (category.id)}
						{@const categoryJobs = getJobsByCategory(category.id, jobsResponse.jobs)}
						{@render jobCategory(category.label, categoryJobs, jobsResponse.isAgent, jobsResponse.durableRuns)}
					{/each}
				</div>
			{/if}
		{/if}
		{#if jobsQuery.error}<div class="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-destructive">
				{jobsQuery.error.message}
			</div>{/if}
	</div>
</section>
