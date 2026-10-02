<script lang="ts">
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { SvelteSet } from 'svelte/reactivity';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { containerService } from '#lib/services/container-service.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import JobRow from '#lib/components/job-card/job-row.svelte';
	import JobDetailPanel from '#lib/components/job-card/job-detail-panel.svelte';
	import { jobNameLabel, type JobPanelSection } from '#lib/components/job-card/job-status.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { EmptyState } from '#lib/components/states/index.js';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as ScrollArea from '#lib/components/ui/scroll-area/index.js';
	import { AlertTriangleIcon, InfoIcon, JobsIcon } from '#lib/icons/index.js';
	import type { JobStatus, JobPrerequisite } from '#lib/types/settings.js';
	import type { ContainerSummaryDto } from '#lib/types/docker.js';
	import type { JobsTabProps } from './tab-props';
	import {
		UPDATE_CHECK_LABEL,
		isAutoUpdateLabelDisabled,
		normalizeContainerName,
		parseExcludedContainerSet
	} from '#lib/utils/container-auto-update.js';

	let { formInputs = $bindable(), environmentId }: JobsTabProps = $props();

	const vulnerabilityManagementEnabled = $derived(featureStore.isEnabled('vulnerabilityManagement', environmentId));
	const canManageJobs = $derived(hasPermission('jobs:manage', environmentId));
	const jobsQuery = createQuery(() => ({
		queryKey: queryKeys.jobs.list(environmentId),
		queryFn: async ({ signal }) => {
			const response = await jobScheduleService.listJobs(environmentId, { signal, suppressAccessDeniedToast: true });
			return {
				...response,
				jobs: response.jobs.map((job) => ({
					...job,
					prerequisites: (job.prerequisites ?? []).map((prereq) => ({ ...prereq, settingsUrl: resolveSettingsUrl(job, prereq) }))
				}))
			};
		},
		enabled: !!environmentId && canManageJobs,
		refetchInterval: 5000
	}));
	const jobsResponse = $derived(jobsQuery.data);
	const capabilityUnknown = $derived(
		!!jobsResponse?.offline && (!jobsResponse.observedAt || jobsResponse.observedAt.startsWith('0001-'))
	);

	const containersPromise = $derived.by(async () => {
		if (!environmentId) return [];
		if (!formInputs.autoUpdate.value && !formInputs.autoHealEnabled.value) return [];
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
		return parseExcludedContainerSet(formInputs.autoUpdateExcludedContainers?.value);
	});

	function resolveSettingsUrl(_job: JobStatus, prereq: JobPrerequisite): string | undefined {
		if (!prereq.settingsUrl) return undefined;
		if (!environmentId) return prereq.settingsUrl;

		const envBase = `/environments/${environmentId}`;
		switch (prereq.settingKey) {
			case 'pollingEnabled':
			case 'autoUpdate':
			case 'scheduledPruneEnabled':
			case 'autoHealEnabled':
				return `${envBase}?tab=jobs`;
			case 'vulnerabilityScanEnabled':
				return undefined;
			case 'imageAutoPatchEnabled':
				return `${envBase}?tab=security`;
			default:
				return prereq.settingsUrl;
		}
	}

	function loadJobs() {
		if (!canManageJobs) return;
		void jobsQuery.refetch();
	}

	function toggleContainerExclusion(containerName: string) {
		if (formInputs.autoUpdateExcludedContainers) {
			formInputs.autoUpdateExcludedContainers.value = toggleExcludedContainerValue(excludedContainers, containerName);
		}
	}

	const autoHealExcludedContainers = $derived.by(() => {
		return parseExcludedContainerSet(formInputs.autoHealExcludedContainers?.value);
	});

	function toggleAutoHealContainerExclusion(containerName: string) {
		if (formInputs.autoHealExcludedContainers) {
			formInputs.autoHealExcludedContainers.value = toggleExcludedContainerValue(autoHealExcludedContainers, containerName);
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

	// The selected job stays set after close so the sheet can animate out before unmounting.
	let selectedJobId = $state<string | null>(null);
	let panelOpen = $state(false);
	let panelSection = $state<JobPanelSection>('overview');
	const visibleJobs = $derived((jobsResponse?.jobs ?? []).filter((job) => !job.managerOnly || environmentId === '0'));
	const selectedJob = $derived(visibleJobs.find((job) => job.id === selectedJobId));

	// Only these jobs have extra settings, and only once they are switched on.
	function jobHasSettings(job: JobStatus): boolean {
		switch (job.id) {
			case 'image-polling':
				return true;
			case 'auto-update':
				return formInputs.autoUpdate.value;
			case 'auto-heal':
				return formInputs.autoHealEnabled.value;
			default:
				return false;
		}
	}

	function selectJob(job: JobStatus, section: JobPanelSection) {
		selectedJobId = job.id;
		panelSection = section;
		panelOpen = true;
	}

	function isJobEnabled(job: JobStatus): boolean {
		return capabilityUnknown ? job.enabled : (getEnabledOverride(job) ?? job.enabled);
	}

	const summary = $derived.by(() => {
		const enabled = visibleJobs.filter(isJobEnabled);
		const failing = enabled.filter((job) => {
			const run = job.currentRun ?? job.lastRun;
			return (
				['failed', 'needs_attention', 'partial'].includes(run?.status ?? '') ||
				['degraded', 'stopped'].includes(job.workerHealth?.status ?? '')
			);
		});
		const running = enabled.filter((job) => ['running', 'retrying', 'queued'].includes(job.currentRun?.status ?? ''));
		let next: { job: JobStatus; at: ReturnType<typeof parseInstant> } | null = null;
		for (const job of enabled) {
			const at = parseInstant(job.nextRun);
			if (!at) continue;
			if (!next || at.epochMilliseconds < next.at!.epochMilliseconds) next = { job, at };
		}
		return { total: visibleJobs.length, enabled: enabled.length, failing: failing.length, running: running.length, next };
	});

	const categories = [
		{ id: 'updates', label: m.updates() },
		{ id: 'monitoring', label: m.jobs_monitoring_heading() },
		{ id: 'maintenance', label: m.maintenance() },
		{ id: 'security', label: m.security() },
		{ id: 'sync', label: m.resource_sync_cap() },
		{ id: 'telemetry', label: m.jobs_telemetry_heading() }
	];

	// Jobs with prerequisites can be turned on or off; the rest are built-in and always run.
	const configurableJobs = $derived(visibleJobs.filter((job) => job.prerequisites.length > 0));
	const systemJobs = $derived(visibleJobs.filter((job) => job.prerequisites.length === 0));

	function getJobsByCategory(categoryId: string): JobStatus[] {
		return configurableJobs.filter((job) => job.category === categoryId);
	}

	function getEnabledOverride(job: JobStatus): boolean | undefined {
		if (!vulnerabilityManagementEnabled && ['vulnerability-scan', 'vulnerability-risk', 'auto-patch'].includes(job.id))
			return false;
		switch (job.id) {
			case 'scheduled-prune':
				return formInputs.scheduledPruneEnabled.value;
			case 'auto-update':
				return formInputs.autoUpdate.value;
			case 'image-polling':
				return formInputs.pollingEnabled.value;
			case 'vulnerability-scan':
				return formInputs.vulnerabilityScanEnabled.value;
			case 'auto-heal':
				return formInputs.autoHealEnabled.value;
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

{#snippet autoUpdateSettings(job: JobStatus)}
	{#if job.id === 'auto-update'}
		<div class="space-y-3">
			<div class="space-y-1">
				<Label>
					{m.excluded_containers()}
					{#await containersPromise then containers}
						<span class="ml-1 font-normal text-muted-foreground">
							({containers.filter((c) => excludedContainers.has(getContainerName(c))).length})
						</span>
					{/await}
				</Label>
				<p class="text-xs text-muted-foreground">{m.auto_update_exclude_description()}</p>
				<p class="text-xs text-muted-foreground">
					{m.auto_update_check_label_hint({ label: `${UPDATE_CHECK_LABEL}=false` })}
				</p>
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
		<div class="space-y-3">
			<div class="flex items-center justify-between gap-3">
				<div class="space-y-1">
					<Label>{m.jobs_image_event_watcher_label()}</Label>
					<p class="text-xs text-muted-foreground">{m.jobs_image_event_watcher_description()}</p>
				</div>
				<Switch id="image-event-watcher-enabled" bind:checked={formInputs.imageEventWatcherEnabled.value} />
			</div>
			<Alert.Root variant="warning" size="sm">
				<AlertTriangleIcon class="size-4" />
				<Alert.Description>{m.jobs_image_event_watcher_warning()}</Alert.Description>
			</Alert.Root>
		</div>
	{/if}
{/snippet}

{#snippet autoHealSettings(job: JobStatus)}
	{#if job.id === 'auto-heal'}
		<div class="space-y-3">
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="space-y-1">
					<Label for="auto-heal-max-restarts">{m.auto_heal_max_restarts_label()}</Label>
					<p class="text-xs text-muted-foreground">{m.auto_heal_max_restarts_description()}</p>
					<Input
						id="auto-heal-max-restarts"
						type="number"
						min="1"
						class="h-8 w-full"
						bind:value={formInputs.autoHealMaxRestarts.value}
					/>
				</div>
				<div class="space-y-1">
					<Label for="auto-heal-restart-window">{m.auto_heal_restart_window_label()}</Label>
					<p class="text-xs text-muted-foreground">{m.auto_heal_restart_window_description()}</p>
					<Input
						id="auto-heal-restart-window"
						type="number"
						min="1"
						class="h-8 w-full"
						bind:value={formInputs.autoHealRestartWindow.value}
					/>
				</div>
			</div>

			<div class="space-y-1">
				<Label>
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
		<Switch aria-label={job.name} bind:checked={formInputs.pollingEnabled.value} />
	{:else if job.id === 'auto-update'}
		<Switch aria-label={job.name} bind:checked={formInputs.autoUpdate.value} disabled={!formInputs.pollingEnabled.value} />
	{:else if job.id === 'scheduled-prune'}
		<Switch aria-label={job.name} bind:checked={formInputs.scheduledPruneEnabled.value} />
	{:else if job.id === 'vulnerability-scan'}
		<Switch
			aria-label={job.name}
			bind:checked={formInputs.vulnerabilityScanEnabled.value}
			disabled={!vulnerabilityManagementEnabled}
		/>
	{:else if job.id === 'auto-heal'}
		<Switch aria-label={job.name} bind:checked={formInputs.autoHealEnabled.value} />
	{/if}
{/snippet}

{#snippet jobGroup(label: string, jobs: JobStatus[], description?: string)}
	<SettingsSection title={label} {description}>
		{#each jobs as job (job.id)}
			<JobRow
				{job}
				{environmentId}
				isAgent={jobsResponse?.isAgent ?? false}
				durableRuns={(jobsResponse?.durableRuns ?? false) || capabilityUnknown}
				enabledOverride={capabilityUnknown ? undefined : getEnabledOverride(job)}
				onSelect={selectJob}
				onScheduleUpdate={loadJobs}
			>
				{#snippet headerAccessory()}
					{@render jobEnableControl(job)}
				{/snippet}
			</JobRow>
		{/each}
	</SettingsSection>
{/snippet}

{#snippet selectedEnableControl()}
	{#if selectedJob}{@render jobEnableControl(selectedJob)}{/if}
{/snippet}

{#snippet jobSettings(job: JobStatus)}
	{@render autoUpdateSettings(job)}
	{@render imagePollingSettings(job)}
	{@render autoHealSettings(job)}
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
	<div class="h-64 w-full overflow-hidden rounded-md border">
		<ScrollArea.Root class="h-full">
			<div class="p-2">
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
										weight="normal"
										variant={container.disabled ? 'muted' : 'default'}
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
			</div>
		</ScrollArea.Root>
	</div>
{/snippet}

<section class="flex w-full min-w-0 flex-col gap-6">
	<header class="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
		<div class="flex items-start gap-3">
			<JobsIcon class="mt-0.5 size-5 text-muted-foreground" />
			<div class="flex flex-col gap-1.5">
				<h2 class="text-lg font-semibold">{m.automations()}</h2>
				<p class="text-sm text-muted-foreground">{m.jobs_environment_scope_description()}</p>
			</div>
		</div>
		{#if jobsResponse}
			<div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
				<Badge variant="gray">{summary.enabled} / {summary.total} {m.common_enabled().toLowerCase()}</Badge>
				{#if summary.failing > 0}
					<Badge variant="red">{summary.failing} {m.jobs_summary_failing().toLowerCase()}</Badge>
				{/if}
				{#if summary.running > 0}
					<Badge variant="blue"
						><Spinner class="size-2.5" />{summary.running} {m.health_next_check_running_now().toLowerCase()}</Badge
					>
				{/if}
				{#if summary.next}
					<span class="tabular-nums">
						{m.jobs_next_run()}: {jobNameLabel(summary.next.job)} · {formatRelativeTime(summary.next.at)}
					</span>
				{/if}
			</div>
		{/if}
	</header>

	{#if jobsResponse}
		{#if capabilityUnknown || !jobsResponse.durableRuns || jobsResponse.offline}
			<Alert.Root variant={jobsResponse.offline ? 'warning-subtle' : 'default'} size="sm">
				<InfoIcon class="size-4" />
				<Alert.Description>
					{#if capabilityUnknown}{m.jobs_capability_unknown()}
					{:else if !jobsResponse.durableRuns}{m.jobs_upgrade_required()}{/if}
					{#if jobsResponse.offline}
						{m.jobs_offline_status()}
						{#if !capabilityUnknown}{m.jobs_last_confirmed()}: {jobsResponse.observedAt}{/if}
					{/if}
				</Alert.Description>
			</Alert.Root>
		{/if}
	{/if}

	{#if jobsQuery.error}
		<Alert.Root variant="destructive-subtle" size="sm">
			<AlertTriangleIcon class="size-4" />
			<Alert.Description>{extractApiErrorMessage(jobsQuery.error)}</Alert.Description>
		</Alert.Root>
	{:else if jobsQuery.isPending}
		<div class="flex h-32 items-center justify-center">
			<Spinner class="size-8" />
		</div>
	{:else if jobsResponse && visibleJobs.length === 0}
		<EmptyState icon={JobsIcon} title={m.jobs_empty_title()} />
	{:else if jobsResponse}
		<div class="flex flex-col gap-8">
			{#each categories as category (category.id)}
				{@const categoryJobs = getJobsByCategory(category.id)}
				{#if categoryJobs.length > 0}
					{@render jobGroup(category.label, categoryJobs)}
				{/if}
			{/each}
			{#if systemJobs.length > 0}
				{@render jobGroup(m.system(), systemJobs, m.jobs_system_description())}
			{/if}
		</div>
	{/if}
</section>

{#if selectedJob && jobsResponse}
	<JobDetailPanel
		job={selectedJob}
		{environmentId}
		isAgent={jobsResponse.isAgent}
		durableRuns={jobsResponse.durableRuns || capabilityUnknown}
		enabledOverride={capabilityUnknown ? undefined : getEnabledOverride(selectedJob)}
		bind:open={panelOpen}
		bind:section={panelSection}
		settings={jobHasSettings(selectedJob) ? jobSettings : undefined}
		enableControl={selectedEnableControl}
		onScheduleUpdate={loadJobs}
	/>
{/if}
