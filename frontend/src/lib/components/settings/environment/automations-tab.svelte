<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';

	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import JobScheduleDialog from '#lib/components/job-card/job-schedule-dialog.svelte';
	import MixedValuesPopover from '#lib/components/settings/mixed-values-popover.svelte';
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
	import { jobAutomationsQuery, jobSchedulesQuery } from '#lib/query/jobs.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import type { ContainerSummary } from '#lib/types/container.js';
	import type { JobScheduleConfig } from '#lib/types/jobschedule.js';
	import type { FormInput } from '#lib/types/ui/form.js';
	import type { MixedSettingValue } from '#lib/types/ui/settings.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		AUTO_UPDATE_LABEL,
		UPDATE_CHECK_LABEL,
		UPDATE_DELAY_LABEL,
		parseExcludedContainerSet
	} from '#lib/utils/container-auto-update.js';
	import { AUTO_HEAL_LABEL, isContainerLabelDisabled, isContainerLabelEnabled } from '#lib/utils/container-labels.js';
	import { listScopes, settleAcrossEnvironments } from '#lib/utils/environments.js';

	import type { AutomationsTabProps } from './tab-props';

	type ScheduleKey = keyof Pick<
		JobScheduleConfig,
		'pollingInterval' | 'autoUpdateInterval' | 'scheduledPruneInterval' | 'vulnerabilityScanInterval' | 'autoHealInterval'
	>;

	type ContainerListKey = 'autoUpdateExcludedContainers' | 'autoHealExcludedContainers';
	type ContainerModeKey = 'autoUpdateMode' | 'autoHealMode';
	type ContainerMode = 'exclude' | 'include';
	type ClearedList = { list: string; mode: ContainerMode | null };

	type LabelState = {
		hint: string;
		selected: boolean;
	};

	type ContainerItem = {
		value: string;
		label: string;
		disabled?: boolean;
		hint?: string;
		selected: boolean;
	};

	let {
		formInputs = $bindable(),
		mixed,
		lock,
		environments,
		containerModesSupported,
		containerModesKnown
	}: AutomationsTabProps = $props();

	const queryClient = useQueryClient();
	const vulnerabilityManagementEnabled = $derived(
		environments.some((environment) => featureStore.isEnabled('vulnerabilityManagement', environment.id))
	);

	const scheduleQuery = createQuery(() => jobSchedulesQuery(environments));
	const schedulesByEnvironment = $derived(
		new Map((scheduleQuery.data?.byEnvironment ?? []).map(({ environment, data }) => [environment.id, data]))
	);

	const jobListEnvironments = $derived(environments.filter((environment) => hasPermission('jobs:list', environment.id)));
	const jobListQuery = createQuery(() => ({
		...jobAutomationsQuery(jobListEnvironments),
		enabled: jobListEnvironments.length > 0
	}));
	const scheduleTimezone = $derived(
		[...new Set((jobListQuery.data?.byEnvironment ?? []).flatMap(({ data }) => (data.timezone ? [data.timezone] : [])))].join(
			', '
		)
	);

	const canEditSchedules = $derived(environments.every((environment) => hasPermission('jobs:update', environment.id)));
	const containerEnvironments = $derived(environments.filter((environment) => hasPermission('containers:list', environment.id)));

	const containersQuery = createQuery(() => ({
		...containerListQuery(containerEnvironments.map((environment) => environment.id).join(','), containerEnvironments, {
			pagination: { page: 1, limit: 100 }
		}),
		enabled: containerEnvironments.length > 0 && (!!formInputs.autoUpdate.value || !!formInputs.autoHealEnabled.value)
	}));
	const containers = $derived([
		...new Map((containersQuery.data?.data ?? []).map((container) => [containerName(container), container])).values()
	]);
	// Rows merge same-named containers from several environments, so a label only decides a row when all of them carry it.
	const containersByName = $derived(Map.groupBy(containersQuery.data?.data ?? [], containerName));

	let updateSearchTerm = $state('');
	let autoHealSearchTerm = $state('');
	let scheduleTarget = $state<{ key: ScheduleKey; name: string; value?: string } | null>(null);
	let scheduleOpen = $state(false);

	// Agents that predate the mode settings do not report them, so their inputs can be missing.
	const autoUpdateMode = $derived<FormInput<ContainerMode | null> | undefined>(formInputs.autoUpdateMode);
	const autoHealMode = $derived<FormInput<ContainerMode | null> | undefined>(formInputs.autoHealMode);
	const autoUpdateIncludeMode = $derived(autoUpdateMode?.value === 'include');
	const autoHealIncludeMode = $derived(autoHealMode?.value === 'include');
	// Older agents read a shared list as an exclusion list, so the mode is only offered when every
	// selected environment supports it, and a list that is not known to be in exclude mode stays read-only then.
	const modesPartlySupported = $derived(!containerModesSupported && containerModesKnown);
	const autoUpdateListLocked = $derived(modesPartlySupported && autoUpdateMode?.value !== 'exclude');
	const autoHealListLocked = $derived(modesPartlySupported && autoHealMode?.value !== 'exclude');
	const containerModeOptions = [
		{ value: 'exclude', label: m.container_mode_exclude() },
		{ value: 'include', label: m.container_mode_include() }
	];
	// Switching the mode gives a list the opposite meaning, so it is cleared and restored if the switch is undone.
	let clearedLists = $state<Partial<Record<ContainerListKey, ClearedList>>>({});
	// A list that differs between environments or is set by an environment variable cannot be cleared with the
	// mode change, so the mode stays as it is until the list can change too.
	const autoUpdateModeBlocked = $derived(modeBlocked('autoUpdateExcludedContainers'));
	const autoHealModeBlocked = $derived(modeBlocked('autoHealExcludedContainers'));
	// Standalone containers that are not included are not restarted when a container they depend on is updated.
	const autoUpdateListHelp = $derived(
		[
			m.auto_update_check_label_hint({ label: `${UPDATE_CHECK_LABEL}=false` }),
			autoUpdateIncludeMode ? m.auto_update_include_dependents_hint() : '',
			listCleared('autoUpdateExcludedContainers', autoUpdateMode?.value) ? m.container_list_cleared_hint() : ''
		]
			.filter(Boolean)
			.join(' ')
	);
	const autoHealListHelp = $derived(
		listCleared('autoHealExcludedContainers', autoHealMode?.value) ? m.container_list_cleared_hint() : undefined
	);
	const autoUpdateListed = $derived(parseExcludedContainerSet(formInputs.autoUpdateExcludedContainers.value));
	const autoHealListed = $derived(parseExcludedContainerSet(formInputs.autoHealExcludedContainers.value));

	function containerName(container: ContainerSummary): string {
		return container.names[0] || container.id.substring(0, 12);
	}

	function allNamed(container: ContainerSummary, test: (labels?: Record<string, string>) => boolean): boolean {
		return (containersByName.get(containerName(container)) ?? [container]).every((entry) => test(entry.labels));
	}

	function autoUpdateLabelState(container: ContainerSummary): LabelState | undefined {
		// updater=false keeps a container out, so it counts as listed in exclude mode and as unlisted in include mode.
		if (allNamed(container, (labels) => isContainerLabelDisabled(labels, AUTO_UPDATE_LABEL)))
			return { hint: m.jobs_container_label_excluded(), selected: !autoUpdateIncludeMode };
		if (autoUpdateIncludeMode && allNamed(container, (labels) => isContainerLabelEnabled(labels, AUTO_UPDATE_LABEL)))
			return { hint: m.included_by_label(), selected: true };
		return undefined;
	}

	function autoHealLabelState(container: ContainerSummary): LabelState | undefined {
		if (autoHealIncludeMode && allNamed(container, (labels) => isContainerLabelEnabled(labels, AUTO_HEAL_LABEL)))
			return { hint: m.included_by_label(), selected: true };
		return undefined;
	}

	// A label that decides the outcome locks the entry, unless the container is still listed and needs removing.
	function containerItem(
		container: ContainerSummary,
		listed: ReadonlySet<string>,
		labelState: LabelState | undefined,
		listLocked: boolean
	): ContainerItem {
		const name = containerName(container);
		return {
			value: name,
			label: name,
			disabled: listLocked || (!!labelState && !listed.has(name)),
			hint: labelState?.hint,
			selected: listed.has(name) || !!labelState?.selected
		};
	}

	function modeBlocked(listKey: ContainerListKey): boolean {
		return !!mixed(listKey) || (!!lock(listKey)?.locked && !!formInputs[listKey].value.trim());
	}

	function listCleared(listKey: ContainerListKey, mode: ContainerMode | null | undefined): boolean {
		const cleared = clearedLists[listKey];
		return !!cleared && cleared.mode !== mode && !formInputs[listKey].value.trim();
	}

	// The saved copy belongs to the mode it was cleared from, so it is only restored when switching back to that
	// mode with the list still empty. A form reset or new entries leave it unused.
	function changeMode(modeKey: ContainerModeKey, listKey: ContainerListKey, mode: ContainerMode) {
		const modeInput = formInputs[modeKey];
		const listInput = formInputs[listKey];
		const previous = modeInput.value;
		if (previous === mode) return;
		modeInput.value = mode;
		const cleared = clearedLists[listKey];
		if (cleared?.mode === mode && !listInput.value.trim()) {
			listInput.value = cleared.list;
			clearedLists = { ...clearedLists, [listKey]: undefined };
			return;
		}
		clearedLists = { ...clearedLists, [listKey]: listInput.value.trim() ? { list: listInput.value, mode: previous } : undefined };
		listInput.value = '';
	}

	function toggleListed(key: ContainerListKey, current: ReadonlySet<string>, name: string) {
		formInputs[key].value = (current.has(name) ? [...current].filter((entry) => entry !== name) : [...current, name]).join(',');
	}

	function scheduleValues(key: ScheduleKey): string[] {
		return [...new Set(environments.flatMap((environment) => schedulesByEnvironment.get(environment.id)?.[key] ?? []))];
	}

	function mixedSchedules(key: ScheduleKey, name: string): MixedSettingValue[] {
		const groups = Map.groupBy(
			environments.flatMap((environment) => {
				const value = schedulesByEnvironment.get(environment.id)?.[key];
				return value ? [{ value, name: environment.name }] : [];
			}),
			(entry) => entry.value
		);
		return [...groups].map(([value, entries]) => ({
			label: value,
			environments: entries.map((entry) => entry.name),
			apply: () => openSchedule(key, name, value)
		}));
	}

	function openSchedule(key: ScheduleKey, name: string, value?: string) {
		scheduleTarget = { key, name, value };
		scheduleOpen = true;
	}

	async function saveSchedule(update: Record<string, string>) {
		await settleAcrossEnvironments(environments, (environmentId) => jobScheduleService.updateJobSchedules(update, environmentId));
		await Promise.all(
			listScopes(environments.map((environment) => environment.id)).map((scope) =>
				queryClient.invalidateQueries({ queryKey: queryKeys.jobs.list(scope) })
			)
		);
	}
</script>

{#snippet scheduleControl(key: ScheduleKey, name: string)}
	{@const values = scheduleValues(key)}
	{#if values.length > 1}
		<span class="mt-1.5 inline-flex items-center gap-1.5 text-xs text-muted-foreground">
			<ClockIcon class="size-3.5" />
			{m.jobs_schedule()}:
			<MixedValuesPopover
				values={mixedSchedules(key, name)}
				class="rounded-sm font-mono transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
			>
				{m.mixed()}
			</MixedValuesPopover>
			{#if canEditSchedules}
				<button
					type="button"
					title={m.jobs_edit_schedule()}
					class="rounded-sm transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
					onclick={() => openSchedule(key, name)}
				>
					<EditIcon class="size-3" />
				</button>
			{/if}
		</span>
	{:else if values.length > 0}
		<button
			type="button"
			class="mt-1.5 inline-flex items-center gap-1.5 rounded-sm text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
			disabled={!canEditSchedules}
			onclick={() => openSchedule(key, name)}
		>
			<ClockIcon class="size-3.5" />
			{m.jobs_schedule()}:
			<span class="font-mono">{values[0]}</span>
			{#if canEditSchedules}
				<EditIcon class="size-3" />
			{/if}
		</button>
	{/if}
{/snippet}

{#snippet containerExclusionList(config: {
	term: string;
	idPrefix: string;
	item: (container: ContainerSummary) => ContainerItem;
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
		{#snippet children({ describedBy })}
			<Switch
				id="image-event-watcher-enabled"
				bind:checked={formInputs.imageEventWatcherEnabled.value}
				data-mixed={!!mixed('imageEventWatcherEnabled') || undefined}
				aria-describedby={describedBy}
			/>
		{/snippet}
	</SettingsRow>
{/snippet}

{#snippet autoUpdateOptions()}
	{#if modesPartlySupported}
		<p class="text-xs text-muted-foreground">{m.container_mode_unsupported_hint()}</p>
	{:else if autoUpdateMode}
		<SettingsRow
			for="auto-update-mode"
			label={m.auto_update_mode_label()}
			description={m.auto_update_mode_description({ label: `${AUTO_UPDATE_LABEL}=true` })}
			class="p-0"
			helpText={autoUpdateModeBlocked ? m.container_mode_blocked_hint() : undefined}
			lock={lock('autoUpdateMode')}
			mixed={mixed('autoUpdateMode')}
		>
			{#snippet children({ describedBy })}
				<SelectWithLabel
					id="auto-update-mode"
					name="autoUpdateMode"
					hideLabel
					value={autoUpdateMode.value ?? ''}
					label={m.auto_update_mode_label()}
					options={containerModeOptions}
					onValueChange={(v) => changeMode('autoUpdateMode', 'autoUpdateExcludedContainers', v as ContainerMode)}
					placeholder={mixed('autoUpdateMode') ? m.mixed() : undefined}
					disabled={autoUpdateModeBlocked}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	{/if}
	<SettingsRow
		for="auto-update-delay"
		label={m.auto_update_delay_label()}
		description={m.auto_update_delay_description({ label: `${UPDATE_DELAY_LABEL}=<hours>` })}
		error={formInputs.autoUpdateDelay.error}
		class="p-0"
		lock={lock('autoUpdateDelay')}
		mixed={mixed('autoUpdateDelay')}
	>
		{#snippet children({ describedBy, errorId })}
			<Input
				id="auto-update-delay"
				type="number"
				min="0"
				max="8760"
				bind:value={formInputs.autoUpdateDelay.value}
				aria-invalid={!!formInputs.autoUpdateDelay.error}
				placeholder={mixed('autoUpdateDelay') ? m.mixed() : undefined}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
	</SettingsRow>
	<SettingsRow
		for="auto-update-container-search"
		label={autoUpdateIncludeMode ? m.included_containers() : m.excluded_containers()}
		description={autoUpdateIncludeMode ? m.auto_update_include_description() : m.auto_update_exclude_description()}
		helpText={autoUpdateListHelp}
		layout="wide"
		class="p-0"
		lock={lock('autoUpdateExcludedContainers')}
		mixed={mixed('autoUpdateExcludedContainers')}
	>
		{#snippet children({ describedBy })}
			<div class="space-y-2">
				<Input
					id="auto-update-container-search"
					type="search"
					placeholder={m.jobs_search_containers()}
					bind:value={updateSearchTerm}
					aria-describedby={describedBy}
				/>
				{@render containerExclusionList({
					term: updateSearchTerm,
					idPrefix: 'container-',
					item: (container) => containerItem(container, autoUpdateListed, autoUpdateLabelState(container), autoUpdateListLocked),
					onToggle: (name) => toggleListed('autoUpdateExcludedContainers', autoUpdateListed, name)
				})}
			</div>
		{/snippet}
	</SettingsRow>
{/snippet}

{#snippet autoHealOptions()}
	{#if modesPartlySupported}
		<p class="text-xs text-muted-foreground">{m.container_mode_unsupported_hint()}</p>
	{:else if autoHealMode}
		<SettingsRow
			for="auto-heal-mode"
			label={m.auto_heal_mode_label()}
			description={m.auto_heal_mode_description({ label: `${AUTO_HEAL_LABEL}=true` })}
			class="p-0"
			helpText={autoHealModeBlocked ? m.container_mode_blocked_hint() : undefined}
			lock={lock('autoHealMode')}
			mixed={mixed('autoHealMode')}
		>
			{#snippet children({ describedBy })}
				<SelectWithLabel
					id="auto-heal-mode"
					name="autoHealMode"
					hideLabel
					value={autoHealMode.value ?? ''}
					label={m.auto_heal_mode_label()}
					options={containerModeOptions}
					onValueChange={(v) => changeMode('autoHealMode', 'autoHealExcludedContainers', v as ContainerMode)}
					placeholder={mixed('autoHealMode') ? m.mixed() : undefined}
					disabled={autoHealModeBlocked}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	{/if}
	<SettingsRow
		for="auto-heal-max-restarts"
		label={m.auto_heal_max_restarts_label()}
		description={m.auto_heal_max_restarts_description()}
		error={formInputs.autoHealMaxRestarts.error}
		class="p-0"
		lock={lock('autoHealMaxRestarts')}
		mixed={mixed('autoHealMaxRestarts')}
	>
		{#snippet children({ describedBy, errorId })}
			<Input
				id="auto-heal-max-restarts"
				type="number"
				min="1"
				bind:value={formInputs.autoHealMaxRestarts.value}
				aria-invalid={!!formInputs.autoHealMaxRestarts.error}
				placeholder={mixed('autoHealMaxRestarts') ? m.mixed() : undefined}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
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
		{#snippet children({ describedBy, errorId })}
			<Input
				id="auto-heal-restart-window"
				type="number"
				min="1"
				bind:value={formInputs.autoHealRestartWindow.value}
				aria-invalid={!!formInputs.autoHealRestartWindow.error}
				placeholder={mixed('autoHealRestartWindow') ? m.mixed() : undefined}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
	</SettingsRow>
	<SettingsRow
		for="auto-heal-container-search"
		label={autoHealIncludeMode ? m.included_containers() : m.excluded_containers()}
		description={autoHealIncludeMode ? m.auto_heal_include_description() : m.auto_heal_exclude_description()}
		helpText={autoHealListHelp}
		layout="wide"
		class="p-0"
		lock={lock('autoHealExcludedContainers')}
		mixed={mixed('autoHealExcludedContainers')}
	>
		{#snippet children({ describedBy })}
			<div class="space-y-2">
				<Input
					id="auto-heal-container-search"
					type="search"
					placeholder={m.jobs_search_containers()}
					bind:value={autoHealSearchTerm}
					aria-describedby={describedBy}
				/>
				{@render containerExclusionList({
					term: autoHealSearchTerm,
					idPrefix: 'auto-heal-container-',
					item: (container) => containerItem(container, autoHealListed, autoHealLabelState(container), autoHealListLocked),
					onToggle: (name) => toggleListed('autoHealExcludedContainers', autoHealListed, name)
				})}
			</div>
		{/snippet}
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
			{#snippet children({ describedBy })}
				<Switch
					id="polling-enabled"
					bind:checked={formInputs.pollingEnabled.value}
					data-mixed={!!mixed('pollingEnabled') || undefined}
					aria-describedby={describedBy}
				/>
			{/snippet}
			{#snippet labelExtra()}
				{@render scheduleControl('pollingInterval', m.image_update_watcher())}
			{/snippet}
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
			{#snippet children({ describedBy })}
				<Switch
					id="auto-update-enabled"
					bind:checked={formInputs.autoUpdate.value}
					disabled={!formInputs.pollingEnabled.value}
					data-mixed={!!mixed('autoUpdate') || undefined}
					aria-describedby={describedBy}
				/>
			{/snippet}
			{#snippet labelExtra()}
				{@render scheduleControl('autoUpdateInterval', m.auto_update())}
			{/snippet}
		</SettingsRow>

		<SettingsRow
			for="github-token"
			label={m.github_token()}
			description={m.github_token_description()}
			error={formInputs.githubToken.error}
			lock={lock('githubToken')}
			mixed={mixed('githubToken')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="github-token"
					type="password"
					autocomplete="new-password"
					bind:value={formInputs.githubToken.value}
					aria-invalid={!!formInputs.githubToken.error}
					placeholder={mixed('githubToken') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
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
			{#snippet children({ describedBy })}
				<Switch
					id="scheduled-prune-enabled"
					bind:checked={formInputs.scheduledPruneEnabled.value}
					data-mixed={!!mixed('scheduledPruneEnabled') || undefined}
					aria-describedby={describedBy}
				/>
			{/snippet}
			{#snippet labelExtra()}
				{@render scheduleControl('scheduledPruneInterval', m.scheduled_prune())}
			{/snippet}
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
				{#snippet children({ describedBy })}
					<Switch
						id="vulnerability-scan-enabled"
						bind:checked={formInputs.vulnerabilityScanEnabled.value}
						data-mixed={!!mixed('vulnerabilityScanEnabled') || undefined}
						aria-describedby={describedBy}
					/>
				{/snippet}
				{#snippet labelExtra()}
					{@render scheduleControl('vulnerabilityScanInterval', m.activity_type_vulnerability_scan())}
				{/snippet}
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
			{#snippet children({ describedBy })}
				<Switch
					id="auto-heal-enabled"
					bind:checked={formInputs.autoHealEnabled.value}
					data-mixed={!!mixed('autoHealEnabled') || undefined}
					aria-describedby={describedBy}
				/>
			{/snippet}
			{#snippet labelExtra()}
				{@render scheduleControl('autoHealInterval', m.notifications_test_auto_heal_notification())}
			{/snippet}
		</SettingsRow>
	</SettingsSection>
</div>

{#if scheduleTarget}
	{@const values = scheduleValues(scheduleTarget.key)}
	{#key scheduleTarget}
		<JobScheduleDialog
			name={scheduleTarget.name}
			settingsKey={scheduleTarget.key}
			schedule={scheduleTarget.value ?? (values.length > 1 ? '' : (values[0] ?? ''))}
			mixed={!scheduleTarget.value && values.length > 1}
			timezone={scheduleTimezone}
			save={saveSchedule}
			bind:open={scheduleOpen}
			onUpdate={() => (scheduleOpen = false)}
		/>
	{/key}
{/if}
