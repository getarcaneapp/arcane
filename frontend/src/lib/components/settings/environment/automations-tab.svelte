<script lang="ts">
	import { createQueries, createQuery, useQueryClient } from '@tanstack/svelte-query';

	import JobScheduleDialog from '#lib/components/job-card/job-schedule-dialog.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ScrollArea from '#lib/components/ui/scroll-area/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { ClockIcon, EditIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerListQuery } from '#lib/query/containers.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import type { ContainerSummaryDto } from '#lib/types/docker.js';
	import type { JobScheduleConfig } from '#lib/types/settings.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import {
		UPDATE_CHECK_LABEL,
		UPDATE_DELAY_LABEL,
		isAutoUpdateLabelDisabled,
		parseExcludedContainerSet
	} from '#lib/utils/container-auto-update.js';
	import { settleAcrossEnvironments } from '#lib/utils/environments.js';

	import type { AutomationsTabProps } from './tab-props';

	type ScheduleKey = keyof Pick<
		JobScheduleConfig,
		'pollingInterval' | 'autoUpdateInterval' | 'scheduledPruneInterval' | 'vulnerabilityScanInterval' | 'autoHealInterval'
	>;

	type ContainerItem = {
		value: string;
		label: string;
		disabled?: boolean;
		hint?: string;
		selected: boolean;
	};

	let { formInputs = $bindable(), mixed, lock, environments }: AutomationsTabProps = $props();

	const queryClient = useQueryClient();
	const vulnerabilityManagementEnabled = $derived(
		environments.some((environment) => featureStore.isEnabled('vulnerabilityManagement', environment.id))
	);

	const scheduleResults = createQueries(() => ({
		queries: environments.map((environment) => ({
			queryKey: queryKeys.jobs.schedules(environment.id),
			queryFn: () => jobScheduleService.getJobSchedules(environment.id)
		}))
	}));

	const containersQuery = createQuery(() => ({
		...containerListQuery(environments.map((environment) => environment.id).join(','), environments, {
			pagination: { page: 1, limit: 100 }
		}),
		enabled: environments.length > 0 && (!!formInputs.autoUpdate.value || !!formInputs.autoHealEnabled.value)
	}));
	const containers = $derived([
		...new Map((containersQuery.data?.data ?? []).map((container) => [containerName(container), container])).values()
	]);

	let updateSearchTerm = $state('');
	let autoHealSearchTerm = $state('');
	let scheduleTarget = $state<{ key: ScheduleKey; name: string } | null>(null);
	let scheduleOpen = $state(false);

	const autoUpdateExcluded = $derived(parseExcludedContainerSet(formInputs.autoUpdateExcludedContainers.value));
	const autoHealExcluded = $derived(parseExcludedContainerSet(formInputs.autoHealExcludedContainers.value));

	function containerName(container: ContainerSummaryDto): string {
		return container.names[0] || container.id.substring(0, 12);
	}

	function toggleExcluded(current: ReadonlySet<string>, name: string): string {
		return (current.has(name) ? [...current].filter((entry) => entry !== name) : [...current, name]).join(',');
	}

	function scheduleValues(key: ScheduleKey): string[] {
		return [...new Set(scheduleResults.flatMap((result) => (result.data ? [result.data[key]] : [])))];
	}

	function openSchedule(key: ScheduleKey, name: string) {
		scheduleTarget = { key, name };
		scheduleOpen = true;
	}

	async function saveSchedule(update: Record<string, string>) {
		await settleAcrossEnvironments(environments, (environmentId) => jobScheduleService.updateJobSchedules(update, environmentId));
		await Promise.all(
			environments.map((environment) => queryClient.invalidateQueries({ queryKey: queryKeys.jobs.list(environment.id) }))
		);
	}
</script>

{#snippet scheduleControl(key: ScheduleKey, name: string)}
	{@const values = scheduleValues(key)}
	{#if values.length > 0}
		<button
			type="button"
			class="mt-1.5 inline-flex items-center gap-1.5 rounded-sm text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
			onclick={() => openSchedule(key, name)}
		>
			<ClockIcon class="size-3.5" />
			{m.jobs_schedule()}:
			<span class="font-mono">{values.length > 1 ? m.mixed() : values[0]}</span>
			<EditIcon class="size-3" />
		</button>
	{/if}
{/snippet}

{#snippet containerExclusionList(config: {
	term: string;
	idPrefix: string;
	item: (container: ContainerSummaryDto) => ContainerItem;
	onToggle: (name: string) => void;
})}
	<div class="h-64 w-full overflow-hidden rounded-md border">
		<ScrollArea.Root class="h-full">
			<div class="space-y-2 p-2">
				{#if containersQuery.isLoading}
					<div class="flex items-center justify-center p-4">
						<Spinner class="size-4" />
					</div>
				{:else if containersQuery.isError}
					<div class="space-y-1 p-2 text-sm text-destructive">
						<p>{m.jobs_containers_load_error()}</p>
						<p class="text-xs opacity-80">{extractApiErrorMessage(containersQuery.error)}</p>
					</div>
				{:else}
					{@const items = containers
						.map(config.item)
						.filter((item) => item.label.toLowerCase().includes(config.term.toLowerCase()))}
					{#each items as container (container.value)}
						<div class="flex items-center space-x-2">
							<Checkbox
								id="{config.idPrefix}{container.value}"
								checked={container.selected}
								disabled={container.disabled}
								onCheckedChange={() => config.onToggle(container.value)}
							/>
							<Label for="{config.idPrefix}{container.value}" weight="normal" variant={container.disabled ? 'muted' : 'default'}>
								{container.label}
								{#if container.hint}
									<span class="ml-1 text-xs opacity-70">{container.hint}</span>
								{/if}
							</Label>
						</div>
					{:else}
						<p class="py-4 text-center text-sm text-muted-foreground">{m.common_no_results_found()}</p>
					{/each}
				{/if}
			</div>
		</ScrollArea.Root>
	</div>
{/snippet}

{#snippet pollingOptions()}
	<SettingsRow
		for="image-event-watcher-enabled"
		label={m.jobs_image_event_watcher_label()}
		description={m.jobs_image_event_watcher_description()}
		warningText={m.jobs_image_event_watcher_warning()}
		layout="switch"
		class="p-0"
		lock={lock('imageEventWatcherEnabled')}
		mixed={mixed('imageEventWatcherEnabled')}
	>
		<Switch
			id="image-event-watcher-enabled"
			bind:checked={formInputs.imageEventWatcherEnabled.value}
			data-mixed={!!mixed('imageEventWatcherEnabled') || undefined}
		/>
	</SettingsRow>
{/snippet}

{#snippet autoUpdateOptions()}
	<SettingsRow
		for="auto-update-delay"
		label={m.auto_update_delay_label()}
		description={m.auto_update_delay_description({ label: `${UPDATE_DELAY_LABEL}=<hours>` })}
		error={formInputs.autoUpdateDelay.error}
		class="p-0"
		lock={lock('autoUpdateDelay')}
		mixed={mixed('autoUpdateDelay')}
	>
		<Input
			id="auto-update-delay"
			type="number"
			min="0"
			max="8760"
			bind:value={formInputs.autoUpdateDelay.value}
			aria-invalid={!!formInputs.autoUpdateDelay.error}
			placeholder={mixed('autoUpdateDelay') ? m.mixed() : undefined}
		/>
	</SettingsRow>
	<SettingsRow
		for="auto-update-container-search"
		label={m.excluded_containers()}
		description={m.auto_update_exclude_description()}
		helpText={m.auto_update_check_label_hint({ label: `${UPDATE_CHECK_LABEL}=false` })}
		layout="wide"
		class="p-0"
		lock={lock('autoUpdateExcludedContainers')}
		mixed={mixed('autoUpdateExcludedContainers')}
	>
		<div class="space-y-2">
			<Input
				id="auto-update-container-search"
				type="search"
				placeholder={m.jobs_search_containers()}
				bind:value={updateSearchTerm}
			/>
			{@render containerExclusionList({
				term: updateSearchTerm,
				idPrefix: 'container-',
				item: (container) => ({
					value: containerName(container),
					label: containerName(container),
					disabled: isAutoUpdateLabelDisabled(container.labels),
					hint: isAutoUpdateLabelDisabled(container.labels) ? m.jobs_container_label_excluded() : undefined,
					selected: autoUpdateExcluded.has(containerName(container))
				}),
				onToggle: (name) => (formInputs.autoUpdateExcludedContainers.value = toggleExcluded(autoUpdateExcluded, name))
			})}
		</div>
	</SettingsRow>
{/snippet}

{#snippet autoHealOptions()}
	<SettingsRow
		for="auto-heal-max-restarts"
		label={m.auto_heal_max_restarts_label()}
		description={m.auto_heal_max_restarts_description()}
		error={formInputs.autoHealMaxRestarts.error}
		class="p-0"
		lock={lock('autoHealMaxRestarts')}
		mixed={mixed('autoHealMaxRestarts')}
	>
		<Input
			id="auto-heal-max-restarts"
			type="number"
			min="1"
			bind:value={formInputs.autoHealMaxRestarts.value}
			aria-invalid={!!formInputs.autoHealMaxRestarts.error}
			placeholder={mixed('autoHealMaxRestarts') ? m.mixed() : undefined}
		/>
	</SettingsRow>
	<SettingsRow
		for="auto-heal-restart-window"
		label={m.auto_heal_restart_window_label()}
		description={m.auto_heal_restart_window_description()}
		error={formInputs.autoHealRestartWindow.error}
		class="p-0"
		lock={lock('autoHealRestartWindow')}
		mixed={mixed('autoHealRestartWindow')}
	>
		<Input
			id="auto-heal-restart-window"
			type="number"
			min="1"
			bind:value={formInputs.autoHealRestartWindow.value}
			aria-invalid={!!formInputs.autoHealRestartWindow.error}
			placeholder={mixed('autoHealRestartWindow') ? m.mixed() : undefined}
		/>
	</SettingsRow>
	<SettingsRow
		for="auto-heal-container-search"
		label={m.excluded_containers()}
		description={m.auto_heal_exclude_description()}
		layout="wide"
		class="p-0"
		lock={lock('autoHealExcludedContainers')}
		mixed={mixed('autoHealExcludedContainers')}
	>
		<div class="space-y-2">
			<Input
				id="auto-heal-container-search"
				type="search"
				placeholder={m.jobs_search_containers()}
				bind:value={autoHealSearchTerm}
			/>
			{@render containerExclusionList({
				term: autoHealSearchTerm,
				idPrefix: 'auto-heal-container-',
				item: (container) => ({
					value: containerName(container),
					label: containerName(container),
					selected: autoHealExcluded.has(containerName(container))
				}),
				onToggle: (name) => (formInputs.autoHealExcludedContainers.value = toggleExcluded(autoHealExcluded, name))
			})}
		</div>
	</SettingsRow>
{/snippet}

<div class="space-y-8">
	<SettingsSection title={m.updates()}>
		<SettingsRow
			for="polling-enabled"
			label={m.image_update_watcher()}
			description={m.image_update_watcher_description()}
			layout="switch"
			lock={lock('pollingEnabled')}
			mixed={mixed('pollingEnabled')}
			expanded={formInputs.pollingEnabled.value ? pollingOptions : undefined}
		>
			{#snippet labelExtra()}
				{@render scheduleControl('pollingInterval', m.image_update_watcher())}
			{/snippet}
			<Switch
				id="polling-enabled"
				bind:checked={formInputs.pollingEnabled.value}
				data-mixed={!!mixed('pollingEnabled') || undefined}
			/>
		</SettingsRow>

		<SettingsRow
			for="auto-update-enabled"
			label={m.auto_update()}
			description={m.auto_update_description()}
			layout="switch"
			lock={lock('autoUpdate')}
			mixed={mixed('autoUpdate')}
			expanded={formInputs.autoUpdate.value ? autoUpdateOptions : undefined}
		>
			{#snippet labelExtra()}
				{@render scheduleControl('autoUpdateInterval', m.auto_update())}
			{/snippet}
			<Switch
				id="auto-update-enabled"
				bind:checked={formInputs.autoUpdate.value}
				disabled={!formInputs.pollingEnabled.value}
				data-mixed={!!mixed('autoUpdate') || undefined}
			/>
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.maintenance()}>
		<SettingsRow
			for="scheduled-prune-enabled"
			label={m.scheduled_prune()}
			description={m.scheduled_prune_description()}
			layout="switch"
			lock={lock('scheduledPruneEnabled')}
			mixed={mixed('scheduledPruneEnabled')}
		>
			{#snippet labelExtra()}
				{@render scheduleControl('scheduledPruneInterval', m.scheduled_prune())}
			{/snippet}
			<Switch
				id="scheduled-prune-enabled"
				bind:checked={formInputs.scheduledPruneEnabled.value}
				data-mixed={!!mixed('scheduledPruneEnabled') || undefined}
			/>
		</SettingsRow>
	</SettingsSection>

	{#if vulnerabilityManagementEnabled}
		<SettingsSection title={m.security()}>
			<SettingsRow
				for="vulnerability-scan-enabled"
				label={m.activity_type_vulnerability_scan()}
				description={m.vulnerability_scan_description()}
				layout="switch"
				lock={lock('vulnerabilityScanEnabled')}
				mixed={mixed('vulnerabilityScanEnabled')}
			>
				{#snippet labelExtra()}
					{@render scheduleControl('vulnerabilityScanInterval', m.activity_type_vulnerability_scan())}
				{/snippet}
				<Switch
					id="vulnerability-scan-enabled"
					bind:checked={formInputs.vulnerabilityScanEnabled.value}
					data-mixed={!!mixed('vulnerabilityScanEnabled') || undefined}
				/>
			</SettingsRow>
		</SettingsSection>
	{/if}

	<SettingsSection title={m.jobs_monitoring_heading()}>
		<SettingsRow
			for="auto-heal-enabled"
			label={m.notifications_test_auto_heal_notification()}
			description={m.auto_heal_description()}
			layout="switch"
			lock={lock('autoHealEnabled')}
			mixed={mixed('autoHealEnabled')}
			expanded={formInputs.autoHealEnabled.value ? autoHealOptions : undefined}
		>
			{#snippet labelExtra()}
				{@render scheduleControl('autoHealInterval', m.notifications_test_auto_heal_notification())}
			{/snippet}
			<Switch
				id="auto-heal-enabled"
				bind:checked={formInputs.autoHealEnabled.value}
				data-mixed={!!mixed('autoHealEnabled') || undefined}
			/>
		</SettingsRow>
	</SettingsSection>
</div>

{#if scheduleTarget}
	{@const values = scheduleValues(scheduleTarget.key)}
	{#key scheduleTarget}
		<JobScheduleDialog
			name={scheduleTarget.name}
			settingsKey={scheduleTarget.key}
			schedule={values.length > 1 ? '' : (values[0] ?? '')}
			mixed={values.length > 1}
			save={saveSchedule}
			bind:open={scheduleOpen}
			onUpdate={() => (scheduleOpen = false)}
		/>
	{/key}
{/if}
