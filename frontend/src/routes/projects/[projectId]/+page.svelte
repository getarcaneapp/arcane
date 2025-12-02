<script lang="ts">
	import type { Project } from '$lib/types/project.type';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as TreeView from '$lib/components/ui/tree-view/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import FileStackIcon from '@lucide/svelte/icons/file-stack';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import LogsIcon from '@lucide/svelte/icons/logs';
	import FileTextIcon from '@lucide/svelte/icons/file-text';
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
	import { untrack } from 'svelte';
	import { projectService } from '$lib/services/project-service';

	let { data } = $props();
	let projectId = $derived(data.projectId);
	let project = $state(untrack(() => data.project));
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

	let originalName = $state(untrack(() => data.editorState.originalName));
	let originalComposeContent = $state(untrack(() => data.editorState.originalComposeContent));
	let originalEnvContent = $state(untrack(() => data.editorState.originalEnvContent || ''));
	let includeFilesState = $state<Record<string, string>>({});
	let originalIncludeFiles = $state<Record<string, string>>({});

	const formSchema = z.object({
		name: z
			.string()
			.min(1, 'Project name is required')
			.regex(/^[a-z0-9_-]+$/i, 'Only letters, numbers, hyphens, and underscores are allowed'),
		composeContent: z.string().min(1, 'Compose content is required'),
		envContent: z.string().optional().default('')
	});

	let formData = $derived({
		name: editorState.name,
		composeContent: editorState.composeContent,
		envContent: editorState.envContent || ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let hasChanges = $derived(
		$inputs.name.value !== originalName ||
			$inputs.composeContent.value !== originalComposeContent ||
			$inputs.envContent.value !== originalEnvContent ||
			JSON.stringify(includeFilesState) !== JSON.stringify(originalIncludeFiles)
	);

	let canEditName = $derived(!isLoading.saving && project?.status !== 'running' && project?.status !== 'partially running');

	let autoScrollStackLogs = $state(true);

	let selectedTab = $state<'services' | 'compose' | 'logs'>('compose');
	let composeOpen = $state(true);
	let envOpen = $state(true);
	let includeFilesPanelStates = $state<Record<string, boolean>>({});
	let selectedFile = $state<'compose' | 'env' | string>('compose');

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
			icon: SettingsIcon
		},
		{
			value: 'logs',
			label: m.compose_nav_logs(),
			icon: LogsIcon,
			disabled: project?.status !== 'running'
		}
	]);

	let nameInputRef = $state<HTMLInputElement | null>(null);

	type ComposeUIPrefs = {
		tab: 'services' | 'compose' | 'logs';
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
		project = data.project;
	});

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

		// Initialize include file states
		if (project?.includeFiles) {
			const newIncludeState: Record<string, string> = {};
			project.includeFiles.forEach((file) => {
				newIncludeState[file.relativePath] = file.content;
				if (!(file.relativePath in includeFilesPanelStates)) {
					includeFilesPanelStates[file.relativePath] = true;
				}
			});
			includeFilesState = newIncludeState;
			originalIncludeFiles = { ...newIncludeState };
		}
	});

	async function handleSaveChanges() {
		if (!project || !hasChanges) return;

		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;

		// First update the main project files
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.updateProject(projectId, name, composeContent, envContent)),
			message: 'Failed to Save Project',
			setLoadingState: (value) => (isLoading.saving = value),
			onSuccess: async (updatedStack: Project) => {
				// Then update any changed include files
				for (const relativePath of Object.keys(includeFilesState)) {
					if (includeFilesState[relativePath] !== originalIncludeFiles[relativePath]) {
						const includeResult = await tryCatch(
							projectService.updateProjectIncludeFile(projectId, relativePath, includeFilesState[relativePath])
						);
						if (includeResult.error) {
							toast.error(`Failed to update ${relativePath}: ${includeResult.error.message || 'Unknown error'}`);
							return;
						}
					}
				}

				toast.success('Project updated successfully!');
				originalName = updatedStack.name;
				originalComposeContent = $inputs.composeContent.value;
				originalEnvContent = $inputs.envContent.value;
				originalIncludeFiles = { ...includeFilesState };
				await new Promise((resolve) => setTimeout(resolve, 200));
				await invalidateAll();
			}
		});
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

	async function refreshProjectDetails() {
		if (!projectId) return;
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.getProject(projectId)),
			message: m.common_refresh_failed({ resource: m.project() }),
			onSuccess: (updatedProject) => {
				project = updatedProject;
			}
		});
	}
</script>

{#if project}
	<TabbedPageLayout
		backUrl="/projects"
		backLabel={m.common_back()}
		{tabItems}
		{selectedTab}
		onTabChange={(value) => {
			selectedTab = value as 'services' | 'compose' | 'logs';
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
					onRefresh={refreshProjectDetails}
				/>
			</div>
		{/snippet}

		{#snippet tabContent()}
			<Tabs.Content value="services" class="h-full">
				<ServicesGrid services={project.services} {projectId} />
			</Tabs.Content>

			<Tabs.Content value="compose" class="h-full">
				<div class="flex h-full flex-col gap-4 lg:flex-row">
					<div
						class="border-border bg-card flex w-full flex-col overflow-y-auto rounded-lg border lg:h-full lg:w-fit lg:max-w-xs lg:min-w-48"
					>
						<div class="border-border border-b p-3">
							<h3 class="text-sm font-medium">Project Files</h3>
						</div>
						<div class="p-2">
							<TreeView.Root class="p-2">
								<TreeView.File
									name="compose.yaml"
									onclick={() => (selectedFile = 'compose')}
									class={selectedFile === 'compose' ? 'bg-accent' : ''}
								>
									{#snippet icon()}
										<FileTextIcon class="size-4 text-blue-500" />
									{/snippet}
								</TreeView.File>

								<TreeView.File
									name=".env"
									onclick={() => (selectedFile = 'env')}
									class={selectedFile === 'env' ? 'bg-accent' : ''}
								>
									{#snippet icon()}
										<FileTextIcon class="size-4 text-green-500" />
									{/snippet}
								</TreeView.File>

								{#if project?.includeFiles && project.includeFiles.length > 0}
									<TreeView.Folder name="Includes">
										{#each project.includeFiles as includeFile}
											<TreeView.File
												name={includeFile.relativePath}
												onclick={() => (selectedFile = includeFile.relativePath)}
												class={selectedFile === includeFile.relativePath ? 'bg-accent' : ''}
											>
												{#snippet icon()}
													<FileTextIcon class="size-4 text-amber-500" />
												{/snippet}
											</TreeView.File>
										{/each}
									</TreeView.Folder>
								{/if}
							</TreeView.Root>
						</div>
					</div>

					<div class="flex h-full flex-1 flex-col">
						{#if selectedFile === 'compose'}
							<CodePanel
								bind:open={composeOpen}
								title="compose.yaml"
								language="yaml"
								bind:value={$inputs.composeContent.value}
								placeholder={m.compose_compose_placeholder()}
								error={$inputs.composeContent.error ?? undefined}
							/>
						{:else if selectedFile === 'env'}
							<CodePanel
								bind:open={envOpen}
								title=".env"
								language="env"
								bind:value={$inputs.envContent.value}
								placeholder={m.compose_env_placeholder()}
								error={$inputs.envContent.error ?? undefined}
							/>
						{:else}
							{@const includeFile = project?.includeFiles?.find((f) => f.relativePath === selectedFile)}
							{#if includeFile}
								<CodePanel
									bind:open={includeFilesPanelStates[includeFile.relativePath]}
									title={includeFile.relativePath}
									language="yaml"
									bind:value={includeFilesState[includeFile.relativePath]}
									placeholder="# Include file content"
								/>
							{/if}
						{/if}
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
