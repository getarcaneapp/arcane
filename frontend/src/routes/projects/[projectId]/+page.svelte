<script lang="ts">
	import type { Project } from '$lib/types/project.type';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import FileStackIcon from '@lucide/svelte/icons/file-stack';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import LogsIcon from '@lucide/svelte/icons/logs';
	import SquarePenIcon from '@lucide/svelte/icons/square-pen';
	import { type TabItem } from '$lib/components/tab-bar/index.js';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import ActionButtons from '$lib/components/action-buttons.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import { invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { z } from 'zod/v4';
	import { createForm } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { PersistedState } from 'runed';
	import EditableName from '../components/EditableName.svelte';
	import ServicesGrid from '../components/ServicesGrid.svelte';
	import CodePanel from '../components/CodePanel.svelte';
	import ProjectsLogsPanel from '../components/ProjectLogsPanel.svelte';
	import ProjectSettingsPanel from '../components/ProjectSettingsPanel.svelte';
	import { projectService } from '$lib/services/project-service';

	let { data } = $props();
	let projectId = $derived(data.projectId);
	let project = $derived(data.project);
	let editorState = $derived(data.editorState);

	let isLoading = $state({
		deploying: false,
		stopping: false,
		restarting: false,
		removing: false,
		importing: false,
		redeploying: false,
		destroying: false,
		pulling: false,
		saving: false
	});

	let originalName = $state(data.editorState.originalName);
	let originalComposeContent = $state(data.editorState.originalComposeContent);
	let originalEnvContent = $state(data.editorState.originalEnvContent || '');
	let originalSettings = $state(data.editorState.originalSettings || { autoUpdate: null, autoUpdateCron: null });

	const normalizeEmptyValue = (val: string | null | undefined): string | null => {
		return val === '' || val === null || val === undefined ? null : val;
	};

	const formSchema = z.object({
		name: z
			.string()
			.min(1, 'Project name is required')
			.regex(/^[a-z0-9_-]+$/i, 'Only letters, numbers, hyphens, and underscores are allowed'),
		composeContent: z.string().min(1, 'Compose content is required'),
		envContent: z.string().optional().default(''),
		autoUpdate: z.boolean().nullable(),
		autoUpdateCron: z
			.string()
			.nullable()
			.default(null)
			.refine(
				(val) => {
					if (!val || val.trim() === '') return true; // Empty is valid (immediate)

					// Basic 5-part cron regex: minute hour day month weekday
					const cronRegex =
						/^(\*|[0-5]?\d|[0-5]?\d-[0-5]?\d|[0-5]?\d\/[0-5]?\d|\*\/[0-5]?\d|[0-5]?\d(,[0-5]?\d)+)\s+(\*|[01]?\d|2[0-3]|[01]?\d-[01]?\d|[01]?\d\/[01]?\d|\*\/[01]?\d|[01]?\d(,[01]?\d)+)\s+(\*|[1-9]|[12]\d|3[01]|[1-9]-[1-9]|[1-9]\/[1-9]|\*\/[1-9]|[1-9](,[1-9])+)\s+(\*|[1-9]|1[0-2]|[1-9]-[1-9]|[1-9]\/[1-9]|\*\/[1-9]|[1-9](,[1-9])+)\s+(\*|[0-6]|[0-6]-[0-6]|[0-6]\/[0-6]|\*\/[0-6]|[0-6](,[0-6])+)$/;

					return cronRegex.test(val.trim());
				},
				{
					message: 'Invalid cron expression format. Expected 5 fields: minute hour day month weekday'
				}
			)
	});

	let formData = $derived({
		name: editorState.name,
		composeContent: editorState.composeContent,
		envContent: editorState.envContent || '',
		autoUpdate: project?.settings?.autoUpdate ?? null,
		autoUpdateCron: normalizeEmptyValue(project?.settings?.autoUpdateCron ?? null)
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	// Sync settings with form inputs for two-way binding
	// Initialize from project settings
	let autoUpdate = $state<boolean | null>(null);
	let autoUpdateCron = $state<string | null>(null);

	// Initialize from project when available
	$effect(() => {
		if (project?.settings) {
			autoUpdate = project.settings.autoUpdate ?? null;
			autoUpdateCron = normalizeEmptyValue(project.settings.autoUpdateCron ?? null);
		}
	});

	// Sync form inputs with state
	$effect(() => {
		autoUpdate = $inputs.autoUpdate.value;
		autoUpdateCron = $inputs.autoUpdateCron.value;
	});

	// Sync state back to form inputs
	$effect(() => {
		$inputs.autoUpdate.value = autoUpdate;
		$inputs.autoUpdateCron.value = autoUpdateCron;
	});

	let hasChanges = $derived(
		$inputs.name.value !== originalName ||
			$inputs.composeContent.value !== originalComposeContent ||
			$inputs.envContent.value !== originalEnvContent ||
			$inputs.autoUpdate.value !== originalSettings.autoUpdate ||
			normalizeEmptyValue($inputs.autoUpdateCron.value) !== normalizeEmptyValue(originalSettings.autoUpdateCron)
	);

	let canEditName = $derived(!isLoading.saving && project?.status !== 'running' && project?.status !== 'partially running');

	let autoScrollStackLogs = $state(true);

	let selectedTab = $state<'services' | 'compose' | 'logs' | 'settings'>('compose');
	let composeOpen = $state(true);
	let envOpen = $state(true);

	const tabItems = $derived<TabItem[]>([
		{
			value: 'services',
			label: m.compose_nav_services(),
			icon: LayersIcon,
			badge: project?.serviceCount
		},
		{
			value: 'compose',
			label: m.common_configuration(),
			icon: SquarePenIcon
		},
		{
			value: 'logs',
			label: m.compose_nav_logs(),
			icon: LogsIcon,
			disabled: project?.status !== 'running'
		},
		{
			value: 'settings',
			label: m.settings_title(),
			icon: SettingsIcon
		}
	]);

	let nameInputRef = $state<HTMLInputElement | null>(null);

	type ComposeUIPrefs = {
		tab: 'services' | 'compose' | 'logs' | 'settings';
		composeOpen: boolean;
		envOpen: boolean;
		autoScroll: boolean;
	};

	const defaultComposeUIPrefs: ComposeUIPrefs = {
		tab: 'compose',
		composeOpen: true,
		envOpen: true,
		autoScroll: true
	};

	let prefs: PersistedState<ComposeUIPrefs> | null = null;

	$effect(() => {
		if (!project?.id) return;
		prefs = new PersistedState<ComposeUIPrefs>(`arcane.compose.ui:${project.id}`, defaultComposeUIPrefs, {
			storage: 'session',
			syncTabs: false
		});
		const cur = prefs.current ?? {};
		selectedTab = cur.tab ?? defaultComposeUIPrefs.tab;
		composeOpen = cur.composeOpen ?? defaultComposeUIPrefs.composeOpen;
		envOpen = cur.envOpen ?? defaultComposeUIPrefs.envOpen;
		autoScrollStackLogs = cur.autoScroll ?? defaultComposeUIPrefs.autoScroll;
	});

	async function handleSaveChanges() {
		if (!project || !hasChanges) return;

		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent, autoUpdate, autoUpdateCron } = validated;

		handleApiResultWithCallbacks({
			result: await tryCatch(
				projectService.updateProject(projectId, name, composeContent, envContent, {
					autoUpdate,
					autoUpdateCron: normalizeEmptyValue(autoUpdateCron)
				})
			),
			message: 'Failed to Save Project',
			setLoadingState: (value) => (isLoading.saving = value),
			onSuccess: async (updatedStack: Project) => {
				toast.success('Project updated successfully!');
				originalName = updatedStack.name;
				originalComposeContent = $inputs.composeContent.value;
				originalEnvContent = $inputs.envContent.value;
				originalSettings = updatedStack.settings || { autoUpdate: null, autoUpdateCron: null };
				await new Promise((resolve) => setTimeout(resolve, 200));
				await invalidateAll();
			}
		});
	}

	function handleReset() {
		$inputs.name.value = originalName;
		$inputs.composeContent.value = originalComposeContent;
		$inputs.envContent.value = originalEnvContent;
		$inputs.autoUpdate.value = originalSettings.autoUpdate;
		$inputs.autoUpdateCron.value = normalizeEmptyValue(originalSettings.autoUpdateCron);
	}

	function saveNameIfChanged() {
		if ($inputs.name.value === originalName) return;
		const validated = form.validate();
		if (!validated) return;
		handleSaveChanges();
	}

	function persistPrefs() {
		if (!prefs) return;
		prefs.current = {
			tab: selectedTab,
			composeOpen,
			envOpen,
			autoScroll: autoScrollStackLogs
		};
	}
</script>

{#if project}
	<TabbedPageLayout
		backUrl="/projects"
		backLabel={m.common_back()}
		{tabItems}
		{selectedTab}
		onTabChange={(value) => {
			selectedTab = value as 'services' | 'compose' | 'logs' | 'settings';
			persistPrefs();
		}}
	>
		{#snippet headerInfo()}
			<div class="flex items-center gap-2">
				<EditableName
					bind:value={$inputs.name.value}
					bind:ref={nameInputRef}
					variant="inline"
					error={$inputs.name.error ?? undefined}
					originalValue={originalName}
					canEdit={canEditName}
					onCommit={saveNameIfChanged}
					class="hidden sm:block"
				/>
				<EditableName
					bind:value={$inputs.name.value}
					bind:ref={nameInputRef}
					variant="block"
					error={$inputs.name.error ?? undefined}
					originalValue={originalName}
					canEdit={canEditName}
					onCommit={saveNameIfChanged}
					class="block sm:hidden"
				/>
				{#if project.status}
					{@const showTooltip = project.status.toLowerCase() === 'unknown' && project.statusReason}
					<StatusBadge
						variant={getStatusVariant(project.status)}
						text={capitalizeFirstLetter(project.status)}
						tooltip={showTooltip ? project.statusReason : undefined}
					/>
				{/if}
			</div>
			{#if project.createdAt}
				<p class="text-muted-foreground mt-0.5 hidden text-xs sm:block">
					{m.common_created()}: {new Date(project.createdAt ?? '').toLocaleDateString()}
				</p>
			{/if}
		{/snippet}

		{#snippet headerActions()}
			<div class="flex items-center gap-2">
				{#if hasChanges}
					<ArcaneButton
						action="reset"
						loading={isLoading.saving}
						onclick={handleReset}
						disabled={isLoading.saving}
						customLabel={m.common_reset()}
						loadingLabel={m.common_resetting()}
					/>
					<ArcaneButton
						action="save"
						loading={isLoading.saving}
						onclick={handleSaveChanges}
						disabled={!hasChanges}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
					/>
				{/if}
				<ActionButtons
					id={project.id}
					name={project.name}
					type="project"
					itemState={project.status}
					bind:startLoading={isLoading.deploying}
					bind:stopLoading={isLoading.stopping}
					bind:restartLoading={isLoading.restarting}
					bind:removeLoading={isLoading.removing}
					bind:redeployLoading={isLoading.redeploying}
					onActionComplete={() => invalidateAll()}
				/>
			</div>
		{/snippet}

		{#snippet tabContent()}
			<Tabs.Content value="services" class="h-full">
				<ServicesGrid services={project.services} {projectId} />
			</Tabs.Content>

			<Tabs.Content value="compose" class="h-full">
				<div class="grid h-full grid-cols-1 gap-4 lg:grid-cols-5 lg:items-stretch" style="grid-template-rows: 1fr;">
					<div class="flex h-full flex-col lg:col-span-3">
						<CodePanel
							bind:open={composeOpen}
							title={m.compose_compose_file_title()}
							language="yaml"
							bind:value={$inputs.composeContent.value}
							placeholder={m.compose_compose_placeholder()}
							error={$inputs.composeContent.error ?? undefined}
						/>
					</div>

					<div class="flex h-full flex-col lg:col-span-2">
						<CodePanel
							bind:open={envOpen}
							title={m.compose_env_title()}
							language="env"
							bind:value={$inputs.envContent.value}
							placeholder={m.compose_env_placeholder()}
							error={$inputs.envContent.error ?? undefined}
						/>
					</div>
				</div>
			</Tabs.Content>

			<Tabs.Content value="logs" class="h-full">
				{#if project.status == 'running'}
					<ProjectsLogsPanel projectId={project.id} bind:autoScroll={autoScrollStackLogs} />
				{:else}
					<div class="text-muted-foreground py-12 text-center">{m.compose_logs_title()} Unavailable</div>
				{/if}
			</Tabs.Content>

			<Tabs.Content value="settings" class="h-full">
				<ProjectSettingsPanel bind:autoUpdate bind:autoUpdateCron />
			</Tabs.Content>
		{/snippet}
	</TabbedPageLayout>
{:else if !data.error}
	<div class="flex min-h-screen items-center justify-center">
		<div class="text-center">
			<div class="bg-muted/50 mb-6 inline-flex rounded-full p-6">
				<FileStackIcon class="text-muted-foreground size-10" />
			</div>
			<h2 class="mb-3 text-2xl font-medium">{m.common_not_found_title({ resource: m.project() })}</h2>
			<p class="text-muted-foreground mb-8 max-w-md text-center">
				{m.common_not_found_description({ resource: m.project().toLowerCase() })}
			</p>
			<Button variant="outline" href="/projects">
				<ArrowLeftIcon class="mr-2 size-4" />
				{m.common_back_to({ resource: m.projects_title() })}
			</Button>
		</div>
	</div>
{/if}
