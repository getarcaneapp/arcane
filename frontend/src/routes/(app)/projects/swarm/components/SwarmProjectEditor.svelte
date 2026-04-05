<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import * as Card from '$lib/components/ui/card';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as TreeView from '$lib/components/ui/tree-view/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import {
		AddIcon,
		ArrowDownIcon as ChevronDown,
		ArrowLeftIcon,
		CopyIcon,
		FileTextIcon,
		InspectIcon,
		TemplateIcon,
		TerminalIcon
	} from '$lib/icons';
	import { arcaneButtonVariants, actionConfigs } from '$lib/components/arcane-button/variants';
	import { PersistedState } from 'runed';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service.js';
	import { systemService } from '$lib/services/system-service.js';
	import { templateService } from '$lib/services/template-service.js';
	import type { Template } from '$lib/types/template.type';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { preventDefault, createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { toast } from 'svelte-sonner';
	import { untrack } from 'svelte';
	import { z } from 'zod/v4';
	import ResizableSplit from '$lib/components/resizable-split.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte';
	import CodePanel from '../../components/CodePanel.svelte';
	import EditableName from '../../components/EditableName.svelte';
	import type { SwarmProjectEditorPageData } from '../editor-data';
	import type { SwarmStackProjectRuntimeState } from '$lib/types/swarm.type';

	let { data }: { data: SwarmProjectEditorPageData } = $props();

	let saving = $state(false);
	let deploying = $state(false);
	let downing = $state(false);
	let deleting = $state(false);
	let converting = $state(false);
	let creatingTemplate = $state(false);
	let showTemplateDialog = $state(false);
	let showConverterDialog = $state(false);

	const formSchema = z.object({
		name: z
			.string()
			.min(1, m.common_name_required())
			.regex(/^[a-z0-9-_]+$/i, m.compose_project_name_invalid()),
		composeContent: z.string().min(1, m.compose_compose_content_required()),
		envContent: z.string().optional().default('')
	});

	// Initial form values intentionally come from the page load data once.
	// svelte-ignore state_referenced_locally
	const formData = untrack(() => ({
		name: data.stackName ?? (data.selectedTemplate ? data.selectedTemplate.name.toLowerCase().replace(/[^a-z0-9-_]/g, '-') : ''),
		composeContent: data.defaultTemplate || '',
		envContent: data.envTemplate || ''
	}));

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, formData);
	let originalName = $state(formData.name);
	let originalComposeContent = $state(formData.composeContent);
	let originalEnvContent = $state(formData.envContent);
	let lastLoadedSignature = $state<string | null>(null);

	let dockerRunCommand = $state('');
	let composeOpen = $state(true);
	let envOpen = $state(true);
	let nameInputRef = $state<HTMLInputElement | null>(null);
	let selectedFile = $state<'compose' | 'env'>('compose');
	let layoutMode = $state<'classic' | 'tree'>('classic');
	let treePaneWidth = $state(320);
	let composeSplitWidth = $state<number | null>(null);
	let prefs = $state<PersistedState<SwarmEditorPrefs> | null>(null);
	let lastPrefsStorageKey = $state<string | null>(null);
	const isTablet = new IsTablet();
	const minTreePaneWidth = 200;
	const minEditorPaneWidth = 360;
	const minComposePaneWidth = 360;
	const minEnvPaneWidth = 280;

	type SwarmEditorPrefs = {
		layoutMode: 'classic' | 'tree';
		selectedFile: 'compose' | 'env';
		composeOpen: boolean;
		envOpen: boolean;
	};

	const defaultEditorPrefs: SwarmEditorPrefs = {
		layoutMode: 'classic',
		selectedFile: 'compose',
		composeOpen: true,
		envOpen: true
	};

	const globalVariableMap = $derived.by(() =>
		Object.fromEntries((data.globalVariables ?? []).map((item) => [item.key, item.value]))
	);

	function runtimeStateLabel(state: SwarmStackProjectRuntimeState) {
		switch (state) {
			case 'live':
				return 'Live';
			case 'down':
				return 'Down';
			default:
				return 'Unavailable';
		}
	}

	function runtimeStateVariant(state: SwarmStackProjectRuntimeState) {
		switch (state) {
			case 'live':
				return 'green';
			case 'down':
				return 'red';
			default:
				return 'amber';
		}
	}

	const templateBtnClass = arcaneButtonVariants({
		tone: actionConfigs.template?.tone ?? 'outline-primary',
		size: 'default',
		hoverEffect: 'none'
	});

	const dropdownContentClass =
		'arcane-dd-content min-w-[220px] overflow-visible rounded-lg border border-primary/30 bg-background/95 ' +
		'backdrop-blur supports-[backdrop-filter]:bg-background/80 ring-1 ring-inset ring-primary/20 shadow-sm p-1';

	const dropdownItemClass =
		'flex cursor-pointer select-none items-center gap-2 rounded-md px-3 py-2 text-sm ' +
		'text-foreground/90 outline-none transition-colors ' +
		'hover:bg-primary/10 focus:bg-primary/10 ' +
		'data-[disabled]:opacity-50 data-[disabled]:pointer-events-none';

	const hasChanges = $derived(
		$inputs.name.value !== originalName ||
			$inputs.composeContent.value !== originalComposeContent ||
			$inputs.envContent.value !== originalEnvContent
	);
	const hasValidDocument = $derived(!!$inputs.name.value && !!$inputs.composeContent.value);
	const isRuntimeLive = $derived(data.runtimeState === 'live');
	const currentStackName = $derived(data.stackName ?? $inputs.name.value);
	const editorStorageKey = $derived(data.stackName ?? 'new');
	const canSave = $derived(hasChanges && hasValidDocument && !saving && !deploying && !downing && !deleting);
	const canDeploy = $derived(hasValidDocument && !saving && !deploying && !downing && !deleting);
	const canDown = $derived(isRuntimeLive && !saving && !deploying && !downing && !deleting);
	const runtimeHref = $derived($inputs.name.value ? `/swarm/stacks/${encodeURIComponent($inputs.name.value)}` : null);
	const codeEditorContext = $derived({
		envContent: $inputs.envContent.value,
		composeContents: [$inputs.composeContent.value],
		globalVariables: globalVariableMap
	});

	$effect(() => {
		const nextName =
			data.stackName ?? (data.selectedTemplate ? data.selectedTemplate.name.toLowerCase().replace(/[^a-z0-9-_]/g, '-') : '');
		const nextComposeContent = data.defaultTemplate || '';
		const nextEnvContent = data.envTemplate || '';
		const nextSignature = JSON.stringify({
			name: nextName,
			composeContent: nextComposeContent,
			envContent: nextEnvContent
		});

		if (nextSignature === untrack(() => lastLoadedSignature)) {
			return;
		}

		const inputsMatchLoaded = untrack(
			() =>
				$inputs.name.value === nextName &&
				$inputs.composeContent.value === nextComposeContent &&
				$inputs.envContent.value === nextEnvContent
		);
		const canReplaceInputs = inputsMatchLoaded || !untrack(() => hasChanges);

		originalName = nextName;
		originalComposeContent = nextComposeContent;
		originalEnvContent = nextEnvContent;

		if (canReplaceInputs) {
			$inputs.name.value = nextName;
			$inputs.composeContent.value = nextComposeContent;
			$inputs.envContent.value = nextEnvContent;
		}

		lastLoadedSignature = nextSignature;
	});

	function persistPrefs() {
		if (!prefs) return;
		prefs.current = {
			layoutMode,
			selectedFile,
			composeOpen,
			envOpen
		};
	}

	$effect(() => {
		const prefKey = `arcane.swarm.compose.ui:${editorStorageKey}`;
		if (lastPrefsStorageKey === prefKey) return;

		lastPrefsStorageKey = prefKey;
		prefs = new PersistedState<SwarmEditorPrefs>(prefKey, defaultEditorPrefs, {
			storage: 'session',
			syncTabs: false
		});

		const currentPrefs = prefs.current ?? defaultEditorPrefs;
		layoutMode = currentPrefs.layoutMode ?? defaultEditorPrefs.layoutMode;
		selectedFile = currentPrefs.selectedFile ?? defaultEditorPrefs.selectedFile;
		composeOpen = currentPrefs.composeOpen ?? defaultEditorPrefs.composeOpen;
		envOpen = currentPrefs.envOpen ?? defaultEditorPrefs.envOpen;
	});

	$effect(() => {
		selectedFile;
		if (layoutMode === 'tree') {
			persistPrefs();
		}
	});

	async function handleSaveProject() {
		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;
		const stackPathName = currentStackName;

		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.upsertStackProject(stackPathName, { name, composeContent, envContent })),
			message: m.common_save_failed(),
			setLoadingState: (value) => (saving = value),
			onSuccess: async (stackProject) => {
				toast.success(m.common_update_success({ resource: `${m.swarm_stack()} "${stackProject.name}"` }));
				await goto(`/projects/swarm/${encodeURIComponent(stackProject.name)}`, { invalidateAll: true });
			}
		});
	}

	async function handleDeployProject() {
		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;
		const stackPathName = currentStackName;

		await handleApiResultWithCallbacks({
			result: await tryCatch(
				(async () => {
					await swarmService.upsertStackProject(stackPathName, { name, composeContent, envContent });
					return swarmService.deployStack({ name, composeContent, envContent });
				})()
			),
			message: m.common_action_failed(),
			setLoadingState: (value) => (deploying = value),
			onSuccess: async () => {
				toast.success(`${m.swarm_stack()} "${name}" is live.`);
				await goto(`/projects/swarm/${encodeURIComponent(name)}`, { invalidateAll: true });
			}
		});
	}

	async function handleDownProject() {
		const validated = form.validate();
		if (!validated) return;

		const { name } = validated;

		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.downStack(name)),
			message: m.common_action_failed(),
			setLoadingState: (value) => (downing = value),
			onSuccess: async () => {
				toast.success(`${m.swarm_stack()} "${name}" was brought down.`);
				await goto(`/projects/swarm/${encodeURIComponent(name)}`, { invalidateAll: true });
			}
		});
	}

	function handleDeleteProject() {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_stack() }),
			message: `Delete the saved files for "${currentStackName}"?`,
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.deleteStackProject(currentStackName)),
						message: m.common_delete_failed({ resource: `${m.swarm_stack()} "${currentStackName}"` }),
						setLoadingState: (value) => (deleting = value),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_stack()} "${currentStackName}"` }));
							await goto('/projects', { invalidateAll: true });
						}
					});
				}
			}
		});
	}

	async function handleConvertDockerRun() {
		if (!dockerRunCommand.trim()) {
			toast.error(m.compose_enter_docker_run_command());
			return;
		}

		await handleApiResultWithCallbacks({
			result: await tryCatch(systemService.convert(dockerRunCommand)),
			message: m.compose_convert_failed(),
			setLoadingState: (value) => (converting = value),
			onSuccess: (result) => {
				$inputs.composeContent.value = result.dockerCompose;
				$inputs.envContent.value = result.envVars;
				$inputs.name.value = result.serviceName;
				dockerRunCommand = '';
				showConverterDialog = false;
				toast.success(m.compose_convert_success());
			}
		});
	}

	async function handleCreateTemplate() {
		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;

		await handleApiResultWithCallbacks({
			result: await tryCatch(
				templateService.createTemplate({
					name,
					content: composeContent,
					envContent
				})
			),
			message: m.common_create_failed({ resource: `${m.resource_template()} "${name}"` }),
			setLoadingState: (value) => (creatingTemplate = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_template()} "${name}"` }));
			}
		});
	}

	async function handleTemplateSelect(template: Template) {
		showTemplateDialog = false;
		$inputs.composeContent.value = template.content ?? '';
		$inputs.envContent.value = template.envContent ?? '';

		if (!$inputs.name.value?.trim()) {
			$inputs.name.value = template.name.toLowerCase().replace(/[^a-z0-9-_]/g, '-');
		}

		toast.success(m.compose_template_loaded({ name: template.name }));
	}

	const exampleCommands = [m.compose_example_command_1(), m.compose_example_command_2(), m.compose_example_command_3()];
</script>

<div class="bg-background flex h-full min-h-0 flex-col">
	<div class="bg-background/95 sticky top-0 z-10 mb-2 border-b backdrop-blur">
		<div class="mx-auto flex min-h-16 max-w-full flex-col gap-4 px-6 py-3 lg:flex-row lg:items-center lg:justify-between">
			<div class="flex min-w-0 items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					href={data.backHref}
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
				/>
				<div class="bg-border hidden h-4 w-px sm:block"></div>
				<div class="hidden min-w-0 items-center gap-3 sm:flex">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue={data.stackName ?? ''}
						placeholder={m.compose_project_name_placeholder?.() || 'Enter name...'}
						canEdit={data.allowNameEdit && !saving && !deploying && !deleting}
						class="hidden sm:block"
					/>
					<StatusBadge
						text={runtimeStateLabel(data.runtimeState)}
						variant={runtimeStateVariant(data.runtimeState)}
						minWidth="none"
					/>
				</div>
			</div>

			<div class="flex flex-wrap items-center justify-end gap-2">
				{#if hasChanges}
					<ArcaneTooltip.Root open={!$inputs.name.value && !saving && !deploying ? undefined : false}>
						<ArcaneTooltip.Trigger>
							<span class="hidden xl:inline-flex">
								<ArcaneButton action="save" disabled={!canSave} loading={saving} onclick={handleSaveProject} />
							</span>
						</ArcaneTooltip.Trigger>
						<ArcaneTooltip.Content class="arcane-tooltip-content max-w-[280px]">
							<p class="mb-1 text-sm font-medium">{m.compose_project_name_tooltip_title()}</p>
							<p class="text-muted-foreground text-xs">{m.compose_project_name_tooltip_description()}</p>
						</ArcaneTooltip.Content>
					</ArcaneTooltip.Root>

					<ArcaneTooltip.Root open={!$inputs.name.value && !saving && !deploying ? undefined : false}>
						<ArcaneTooltip.Trigger>
							<span class="xl:hidden">
								<ArcaneButton
									action="save"
									size="icon"
									showLabel={false}
									disabled={!canSave}
									loading={saving}
									onclick={handleSaveProject}
								/>
							</span>
						</ArcaneTooltip.Trigger>
						<ArcaneTooltip.Content class="arcane-tooltip-content max-w-[280px]">
							<p class="mb-1 text-sm font-medium">{m.compose_project_name_tooltip_title()}</p>
							<p class="text-muted-foreground text-xs">{m.compose_project_name_tooltip_description()}</p>
						</ArcaneTooltip.Content>
					</ArcaneTooltip.Root>
				{/if}

				{#if !isRuntimeLive}
					<ArcaneButton
						action="deploy"
						disabled={!canDeploy}
						loading={deploying}
						onclick={handleDeployProject}
						class="hidden xl:inline-flex"
					/>
					<ArcaneButton
						action="deploy"
						size="icon"
						showLabel={false}
						disabled={!canDeploy}
						loading={deploying}
						onclick={handleDeployProject}
						class="xl:hidden"
					/>
				{:else}
					<ArcaneButton
						action="stop"
						customLabel={m.common_down()}
						disabled={!canDown}
						loading={downing}
						onclick={handleDownProject}
						class="hidden xl:inline-flex"
					/>
					<ArcaneButton
						action="stop"
						size="icon"
						showLabel={false}
						customLabel={m.common_down()}
						disabled={!canDown}
						loading={downing}
						onclick={handleDownProject}
						class="xl:hidden"
					/>
				{/if}

				{#if runtimeHref && isRuntimeLive}
					<ArcaneButton
						action="inspect"
						href={runtimeHref}
						customLabel="Open runtime"
						icon={InspectIcon}
						class="hidden xl:inline-flex"
					/>
					<ArcaneButton
						action="inspect"
						size="icon"
						showLabel={false}
						href={runtimeHref}
						customLabel="Open runtime"
						icon={InspectIcon}
						class="xl:hidden"
					/>
				{/if}

				{#if data.isExistingProject}
					<ArcaneButton
						action="remove"
						customLabel={m.common_delete()}
						disabled={deleting || deploying || downing || saving}
						loading={deleting}
						onclick={handleDeleteProject}
						class="hidden xl:inline-flex"
					/>
					<ArcaneButton
						action="remove"
						size="icon"
						showLabel={false}
						customLabel={m.common_delete()}
						disabled={deleting || deploying || downing || saving}
						loading={deleting}
						onclick={handleDeleteProject}
						class="xl:hidden"
					/>
				{/if}

				<ButtonGroup.Root>
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<ArcaneButton
									{...props}
									action="base"
									tone="ghost"
									class={templateBtnClass}
									icon={ChevronDown}
									customLabel="More"
								/>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end" class={dropdownContentClass}>
							<DropdownMenu.Group>
								<DropdownMenu.Item
									class={dropdownItemClass}
									disabled={saving || deploying || deleting}
									onclick={() => (showTemplateDialog = true)}
								>
									<TemplateIcon class="size-4" />
									{m.common_use_template()}
								</DropdownMenu.Item>
								<DropdownMenu.Item class={dropdownItemClass} onclick={() => (showConverterDialog = true)}>
									<TerminalIcon class="size-4" />
									{m.compose_convert_from_docker_run()}
								</DropdownMenu.Item>
								<DropdownMenu.Separator />
								<DropdownMenu.Item
									class={dropdownItemClass}
									disabled={!hasValidDocument || creatingTemplate}
									onclick={handleCreateTemplate}
								>
									<AddIcon class="size-4" />
									{m.templates_create_template()}
								</DropdownMenu.Item>
							</DropdownMenu.Group>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</ButtonGroup.Root>
			</div>
		</div>

		<div class="px-6 pb-3 sm:hidden">
			<div class="space-y-3">
				<EditableName
					bind:value={$inputs.name.value}
					bind:ref={nameInputRef}
					variant="block"
					error={$inputs.name.error ?? undefined}
					originalValue={data.stackName ?? ''}
					placeholder={m.compose_project_name_placeholder()}
					canEdit={data.allowNameEdit && !saving && !deploying && !deleting}
				/>
				<StatusBadge
					text={runtimeStateLabel(data.runtimeState)}
					variant={runtimeStateVariant(data.runtimeState)}
					minWidth="none"
				/>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto flex h-full min-h-0 w-full max-w-full min-w-0 flex-1 flex-col">
			<form class="flex h-full min-h-0 flex-1 flex-col gap-4 px-2 sm:px-6" onsubmit={preventDefault(handleSaveProject)}>
				<div class="shrink-0">
					<SwitchWithLabel
						id="swarm-layout-mode-toggle"
						checked={layoutMode === 'tree'}
						label={layoutMode === 'tree' ? m.tree_view() : m.classic()}
						description={m.project_view_description()}
						onCheckedChange={(checked) => {
							layoutMode = checked ? 'tree' : 'classic';
							if (checked) {
								selectedFile = 'compose';
							}
							persistPrefs();
						}}
					/>
				</div>

				<div class="flex h-full min-h-0 flex-1 flex-col">
					{#if layoutMode === 'tree'}
						{#if isTablet.current}
							<div class="flex h-full min-h-0 flex-col gap-4">
								<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
									<Card.Header icon={FileTextIcon} class="flex-shrink-0 items-center">
										<Card.Title>
											<h2>{m.project_files()}</h2>
										</Card.Title>
									</Card.Header>
									<Card.Content class="min-h-0 flex-1 overflow-auto p-2">
										<TreeView.Root class="min-w-max p-2 whitespace-nowrap">
											<TreeView.File
												name="compose.yaml"
												onclick={() => {
													selectedFile = 'compose';
													persistPrefs();
												}}
												class={selectedFile === 'compose' ? 'bg-accent' : ''}
											>
												{#snippet icon()}
													<FileTextIcon class="size-4 text-blue-500" />
												{/snippet}
											</TreeView.File>

											<TreeView.File
												name=".env"
												onclick={() => {
													selectedFile = 'env';
													persistPrefs();
												}}
												class={selectedFile === 'env' ? 'bg-accent' : ''}
											>
												{#snippet icon()}
													<FileTextIcon class="size-4 text-green-500" />
												{/snippet}
											</TreeView.File>
										</TreeView.Root>
									</Card.Content>
								</Card.Root>

								<div class="flex min-h-0 flex-1 flex-col">
									{#if selectedFile === 'compose'}
										<CodePanel
											bind:open={composeOpen}
											title="compose.yaml"
											language="yaml"
											bind:value={$inputs.composeContent.value}
											error={$inputs.composeContent.error ?? undefined}
											fileId={`projects:swarm:${editorStorageKey}:compose`}
											originalValue={originalComposeContent}
											enableDiff={true}
											editorContext={codeEditorContext}
										/>
									{:else}
										<CodePanel
											bind:open={envOpen}
											title=".env"
											language="env"
											bind:value={$inputs.envContent.value}
											error={$inputs.envContent.error ?? undefined}
											fileId={`projects:swarm:${editorStorageKey}:env`}
											originalValue={originalEnvContent}
											enableDiff={true}
											editorContext={codeEditorContext}
										/>
									{/if}
								</div>
							</div>
						{:else}
							<ResizableSplit
								class="h-full min-h-0 lg:gap-2"
								firstClass="flex min-h-0 flex-col"
								secondClass="flex min-h-0 flex-col"
								bind:size={treePaneWidth}
								minSize={minTreePaneWidth}
								minSecondSize={minEditorPaneWidth}
								defaultRatio={0.3}
								ariaLabel="Resize swarm project files panel"
								persistKey={`arcane.swarm.compose.split:${editorStorageKey}:tree`}
								onResizeEnd={persistPrefs}
							>
								{#snippet first()}
									<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
										<Card.Header icon={FileTextIcon} class="shrink-0 items-center">
											<Card.Title>
												<h2>{m.project_files()}</h2>
											</Card.Title>
										</Card.Header>
										<Card.Content class="min-h-0 flex-1 overflow-auto p-2">
											<TreeView.Root class="min-w-max p-2 whitespace-nowrap">
												<TreeView.File
													name="compose.yaml"
													onclick={() => {
														selectedFile = 'compose';
														persistPrefs();
													}}
													class={selectedFile === 'compose' ? 'bg-accent' : ''}
												>
													{#snippet icon()}
														<FileTextIcon class="size-4 text-blue-500" />
													{/snippet}
												</TreeView.File>

												<TreeView.File
													name=".env"
													onclick={() => {
														selectedFile = 'env';
														persistPrefs();
													}}
													class={selectedFile === 'env' ? 'bg-accent' : ''}
												>
													{#snippet icon()}
														<FileTextIcon class="size-4 text-green-500" />
													{/snippet}
												</TreeView.File>
											</TreeView.Root>
										</Card.Content>
									</Card.Root>
								{/snippet}

								{#snippet second()}
									<div class="flex h-full min-h-0 flex-1 flex-col">
										{#if selectedFile === 'compose'}
											<CodePanel
												bind:open={composeOpen}
												title="compose.yaml"
												language="yaml"
												bind:value={$inputs.composeContent.value}
												error={$inputs.composeContent.error ?? undefined}
												fileId={`projects:swarm:${editorStorageKey}:compose`}
												originalValue={originalComposeContent}
												enableDiff={true}
												editorContext={codeEditorContext}
											/>
										{:else}
											<CodePanel
												bind:open={envOpen}
												title=".env"
												language="env"
												bind:value={$inputs.envContent.value}
												error={$inputs.envContent.error ?? undefined}
												fileId={`projects:swarm:${editorStorageKey}:env`}
												originalValue={originalEnvContent}
												enableDiff={true}
												editorContext={codeEditorContext}
											/>
										{/if}
									</div>
								{/snippet}
							</ResizableSplit>
						{/if}
					{:else if isTablet.current}
						<div class="flex min-h-0 flex-1 flex-col gap-4">
							<CodePanel
								bind:open={composeOpen}
								title="compose.yaml"
								language="yaml"
								bind:value={$inputs.composeContent.value}
								error={$inputs.composeContent.error ?? undefined}
								fileId={`projects:swarm:${editorStorageKey}:compose`}
								originalValue={originalComposeContent}
								enableDiff={true}
								editorContext={codeEditorContext}
							/>
							<CodePanel
								bind:open={envOpen}
								title=".env"
								language="env"
								bind:value={$inputs.envContent.value}
								error={$inputs.envContent.error ?? undefined}
								fileId={`projects:swarm:${editorStorageKey}:env`}
								originalValue={originalEnvContent}
								enableDiff={true}
								editorContext={codeEditorContext}
							/>
						</div>
					{:else}
						<ResizableSplit
							class="min-h-0 flex-1 lg:gap-2"
							firstClass="flex min-h-0 flex-col"
							secondClass="flex min-h-0 flex-col"
							bind:size={composeSplitWidth}
							minSize={minComposePaneWidth}
							minSecondSize={minEnvPaneWidth}
							defaultRatio={0.6}
							ariaLabel="Resize swarm compose and env editors"
							persistKey={`arcane.swarm.compose.split:${editorStorageKey}:classic`}
							onResizeEnd={persistPrefs}
						>
							{#snippet first()}
								<div class="flex min-h-0 flex-1 flex-col">
									<CodePanel
										bind:open={composeOpen}
										title="compose.yaml"
										language="yaml"
										bind:value={$inputs.composeContent.value}
										error={$inputs.composeContent.error ?? undefined}
										fileId={`projects:swarm:${editorStorageKey}:compose`}
										originalValue={originalComposeContent}
										enableDiff={true}
										editorContext={codeEditorContext}
									/>
								</div>
							{/snippet}

							{#snippet second()}
								<div class="flex min-h-0 flex-1 flex-col">
									<CodePanel
										bind:open={envOpen}
										title=".env"
										language="env"
										bind:value={$inputs.envContent.value}
										error={$inputs.envContent.error ?? undefined}
										fileId={`projects:swarm:${editorStorageKey}:env`}
										originalValue={originalEnvContent}
										enableDiff={true}
										editorContext={codeEditorContext}
									/>
								</div>
							{/snippet}
						</ResizableSplit>
					{/if}
				</div>
			</form>
		</div>
	</div>
</div>

<Dialog.Root bind:open={showConverterDialog}>
	<Dialog.Content class="max-h-[80vh] sm:max-w-[800px]">
		<Dialog.Header>
			<Dialog.Title>{m.compose_converter_title()}</Dialog.Title>
			<Dialog.Description>{m.compose_converter_description()}</Dialog.Description>
		</Dialog.Header>

		<div class="max-h-[60vh] space-y-4 overflow-y-auto">
			<div class="space-y-2">
				<Label for="dockerRunCommand">{m.compose_docker_run_command_label()}</Label>
				<Textarea
					id="dockerRunCommand"
					bind:value={dockerRunCommand}
					placeholder={m.compose_docker_run_placeholder()}
					rows={3}
					disabled={converting}
					class="font-mono text-sm"
				/>
			</div>

			<div class="space-y-2">
				<Label class="text-muted-foreground text-xs">{m.compose_example_commands_label()}</Label>
				<div class="space-y-1">
					{#each exampleCommands as command (command)}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							class="h-auto w-full justify-start p-2 text-left font-mono text-xs break-all whitespace-normal"
							onclick={() => (dockerRunCommand = command)}
							icon={CopyIcon}
							customLabel={command}
						/>
					{/each}
				</div>
			</div>
		</div>

		<div class="flex w-full justify-end pt-4">
			<ArcaneButton
				action="create"
				disabled={!dockerRunCommand.trim() || converting}
				onclick={handleConvertDockerRun}
				loading={converting}
				customLabel={m.compose_convert_action()}
				loadingLabel={m.compose_converting()}
			/>
		</div>
	</Dialog.Content>
</Dialog.Root>

<TemplateSelectionDialog
	bind:open={showTemplateDialog}
	templates={data.composeTemplates || []}
	onSelect={handleTemplateSelect}
	onDownloadSuccess={invalidateAll}
/>
