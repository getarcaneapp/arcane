<script lang="ts">
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import IconImage from '$lib/components/icon-image.svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { m } from '$lib/paraglide/messages.js';
	import { templateService } from '$lib/services/template-service';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { createForm } from '$lib/utils/settings';
	import ComposeTemplateEditor from '$lib/components/ComposeTemplateEditor.svelte';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import {
		createNamedTemplateSchema,
		getTemplateEditorSaveState,
		resetTemplateEditorFields,
		runTemplateEditorSave
	} from '$lib/utils/template-editor';
	import {
		ArrowLeftIcon,
		ProjectsIcon,
		CodeIcon,
		TemplateIcon,
		DownloadIcon,
		GlobeIcon,
		ContainersIcon,
		BoxIcon,
		VariableIcon,
		FileTextIcon
	} from '$lib/icons';

	let { data } = $props();

	let template = $derived(data.templateData.template);
	let services = $derived(data.templateData.services);
	let envVars = $derived(data.templateData.envVariables);

	// Edit state (custom templates only)
	let status = $state({
		saving: false,
		isDeleting: false,
		isDownloading: false
	});
	let validation = $state({
		composeHasErrors: false,
		envHasErrors: false,
		composeValidationReady: false,
		envValidationReady: false
	});

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));

	// Form schema for custom template editing
	const formSchema = createNamedTemplateSchema();

	let originalName = $state(untrack(() => template.name));
	let originalDescription = $state(untrack(() => template.description ?? ''));
	let originalCompose = $state(untrack(() => data.templateData.content));
	let originalEnv = $state(untrack(() => data.templateData.envContent));

	let formData = $derived({
		name: originalName,
		description: originalDescription,
		composeContent: originalCompose,
		envContent: originalEnv
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	const hasChanges = $derived(
		$inputs.name.value !== originalName ||
			$inputs.description.value !== originalDescription ||
			$inputs.composeContent.value !== originalCompose ||
			$inputs.envContent.value !== originalEnv
	);
	// fallow-ignore-next-line code-duplication template editor form wiring (createForm + getTemplateEditorSaveState); hasChanges fields differ per page
	const saveState = $derived(getTemplateEditorSaveState(validation, hasChanges));
	const validationState = $derived(saveState.validationState);
	const canSave = $derived(saveState.canSave);

	async function handleSave() {
		await runTemplateEditorSave({
			validationState,
			validate: form.validate,
			save: (validated) =>
				templateService.updateTemplate(template.id, {
					name: validated.name,
					description: validated.description,
					content: validated.composeContent,
					envContent: validated.envContent
				}),
			failureMessage: m.templates_save_template_failed(),
			setLoading: (value) => (status.saving = value),
			onSuccess: async (validated) => {
				toast.success(m.templates_save_template_success({ name: validated.name }));
				originalName = validated.name;
				originalDescription = validated.description ?? '';
				originalCompose = validated.composeContent;
				originalEnv = validated.envContent ?? '';
				await invalidateAll();
			}
		});
	}

	function handleReset() {
		resetTemplateEditorFields([
			{
				set: (value) => ($inputs.name.value = value),
				value: originalName
			},
			{
				set: (value) => ($inputs.description.value = value),
				value: originalDescription
			},
			{
				set: (value) => ($inputs.composeContent.value = value),
				value: originalCompose
			},
			{
				set: (value) => ($inputs.envContent.value = value),
				value: originalEnv
			}
		]);
	}

	// Read-only view helpers (remote templates)
	const localVersionOfRemote = $derived.by(() => {
		if (!template.isRemote || !template.metadata?.remoteUrl) return null;
		return data.allTemplates.find((t) => !t.isRemote && t.metadata?.remoteUrl === template.metadata?.remoteUrl);
	});

	const canDownload = $derived(template.isRemote && !localVersionOfRemote);

	async function handleDownload() {
		if (status.isDownloading || !canDownload) return;
		status.isDownloading = true;
		try {
			const downloadedTemplate = await templateService.download(template.id);
			toast.success(m.templates_downloaded_success({ name: template.name }));
			if (downloadedTemplate?.id) {
				await goto(`/customize/templates/${downloadedTemplate.id}`, { replaceState: true });
			} else {
				await invalidateAll();
			}
		} catch (error) {
			console.error('Error downloading template:', error);
			toast.error(error instanceof Error ? error.message : m.templates_download_failed());
		} finally {
			status.isDownloading = false;
		}
	}

	async function handleDelete() {
		if (status.isDeleting) return;
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_template() }),
			message: m.common_delete_confirm({ resource: `${m.resource_template()} "${template.name}"` }),
			confirm: {
				label: m.templates_delete_template(),
				destructive: true,
				action: async () => {
					status.isDeleting = true;
					try {
						await templateService.deleteTemplate(template.id);
						toast.success(m.common_delete_success({ resource: `${m.resource_template()} "${template.name}"` }));
						await goto('/customize/templates');
					} catch (error) {
						console.error('Error deleting template:', error);
						toast.error(
							error instanceof Error
								? error.message
								: m.common_delete_failed({ resource: `${m.resource_template()} "${template.name}"` })
						);
						status.isDeleting = false;
					}
				}
			}
		});
	}
</script>

<div class="mx-auto flex h-full min-h-0 w-full max-w-full flex-col gap-6 overflow-hidden p-2 pb-10 sm:p-6 sm:pb-10">
	<!-- Header -->
	<!-- fallow-ignore-next-line code-duplication page header/back chrome differs per template page (editable vs static header) -->
	<div class="flex-shrink-0 space-y-3 sm:space-y-4">
		<ArcaneButton action="base" tone="ghost" onclick={() => goto('/customize/templates')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.templates_title() })}</span>
		</ArcaneButton>

		{#if !template.isRemote}
			<!-- Editable header for custom templates -->
			<div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
				<div class="min-w-0 flex-1 space-y-3">
					<FormInput
						input={$inputs.name}
						label={m.templates_template_name_label()}
						placeholder={m.templates_template_name_placeholder()}
						disabled={status.saving}
					/>
					<FormInput
						input={$inputs.description}
						label={m.templates_template_description_label()}
						placeholder={m.templates_template_description_placeholder()}
						disabled={status.saving}
					/>
				</div>
				<div class="flex flex-col gap-2 sm:flex-row sm:items-start">
					<ArcaneButton
						action="create"
						onclick={() => goto(`/projects/new?templateId=${template.id}`)}
						customLabel={m.compose_create_project()}
						class="w-full gap-2 sm:w-auto"
					/>
					<ArcaneButton action="cancel" onclick={handleReset} disabled={!hasChanges || status.saving}>
						{m.common_reset()}
					</ArcaneButton>
					<ArcaneButton
						action="save"
						onclick={handleSave}
						disabled={!canSave}
						loading={status.saving}
						loadingLabel={m.common_action_saving()}
					/>
					<ArcaneButton
						action="remove"
						onclick={handleDelete}
						disabled={status.isDeleting}
						loading={status.isDeleting}
						loadingLabel={m.common_action_deleting()}
						customLabel={m.templates_delete_template()}
						class="w-full gap-2 sm:w-auto"
					/>
				</div>
			</div>

			<div class="flex flex-wrap items-center gap-2">
				<Badge variant="secondary" class="gap-1">
					<TemplateIcon class="size-3" />
					{m.templates_local()}
				</Badge>
			</div>
		{:else}
			<!-- Read-only header for remote templates -->
			<div class="flex min-w-0 items-start gap-3">
				<IconImage
					src={template.metadata?.iconUrl}
					alt={template.name}
					fallback={GlobeIcon}
					class="size-6"
					containerClass="size-9 bg-transparent ring-0"
				/>
				<div class="min-w-0 flex-1">
					<h1 class="text-xl font-semibold break-words sm:text-2xl">{template.name}</h1>
					{#if template.description}
						<p class="text-muted-foreground mt-1.5 text-sm break-words sm:text-base">{template.description}</p>
					{/if}
				</div>
			</div>

			<div class="flex flex-wrap items-center gap-2">
				<Badge variant="secondary" class="gap-1">
					<GlobeIcon class="size-3" />
					{m.templates_remote()}
				</Badge>
				{#if template.metadata?.tags && template.metadata.tags.length > 0}
					{#each template.metadata.tags as tag (tag)}
						<Badge variant="outline">{tag}</Badge>
					{/each}
				{/if}
			</div>

			<div class="flex flex-col gap-2 sm:flex-row">
				<ArcaneButton
					action="create"
					onclick={() => goto(`/projects/new?templateId=${template.id}`)}
					customLabel={m.compose_create_project()}
					class="w-full gap-2 sm:w-auto"
				/>
				{#if canDownload}
					<ArcaneButton
						action="base"
						onclick={handleDownload}
						disabled={status.isDownloading}
						loading={status.isDownloading}
						loadingLabel={m.common_action_downloading()}
						class="w-full gap-2 sm:w-auto"
					>
						<DownloadIcon class="size-4" />
						{m.templates_download()}
					</ArcaneButton>
				{:else if template.isRemote && localVersionOfRemote}
					<ArcaneButton
						action="base"
						onclick={() => goto(`/customize/templates/${localVersionOfRemote?.id}`)}
						class="w-full gap-2 sm:w-auto"
					>
						<ProjectsIcon class="size-4" />
						{m.templates_view_local_version()}
					</ArcaneButton>
				{/if}
			</div>
		{/if}
	</div>

	{#if !template.isRemote}
		<!-- Edit layout: compose editor + env editor side by side -->
		<ComposeTemplateEditor
			bind:composeValue={$inputs.composeContent.value}
			bind:envValue={$inputs.envContent.value}
			{originalCompose}
			{originalEnv}
			bind:validation
			{globalVariableMap}
			fileIdPrefix="templates:custom:{template.id}"
			readOnly={status.saving}
			composeError={$inputs.composeContent.error}
			envError={$inputs.envContent.error}
		/>
	{:else}
		<!-- Read-only view for remote templates -->
		<div class="grid flex-shrink-0 gap-4 sm:grid-cols-2">
			<Card.Root variant="subtle">
				<Card.Content class="flex items-center gap-4 p-4">
					<div class="flex size-12 shrink-0 items-center justify-center rounded-lg bg-blue-500/10">
						<ContainersIcon class="size-6 text-blue-500" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">{m.compose_services()}</div>
						<div class="mt-1">
							<div class="text-2xl font-semibold">{services?.length ?? 0}</div>
						</div>
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex items-center gap-4 p-4">
					<div class="flex size-12 shrink-0 items-center justify-center rounded-lg bg-purple-500/10">
						<VariableIcon class="size-6 text-purple-500" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.common_environment_variables()}
						</div>
						<div class="mt-1 flex flex-wrap items-baseline gap-2">
							<div class="text-2xl font-semibold">{envVars?.length ?? 0}</div>
							{#if envVars?.length}
								<div class="text-muted-foreground text-sm">{m.templates_configurable_settings()}</div>
							{/if}
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		</div>

		<div class="min-h-0 flex-1">
			<div class="grid h-full min-h-0 gap-6 lg:grid-cols-2 xl:grid-cols-3">
				<div
					class="border-border/70 flex h-full min-h-0 min-w-0 flex-col overflow-hidden rounded-xl border lg:col-span-1 xl:col-span-2"
				>
					<div class="border-border/50 flex-shrink-0 border-b px-4 py-2.5">
						<div class="flex items-center gap-2">
							<CodeIcon class="text-primary size-4 shrink-0" />
							<h2 class="text-sm font-semibold">{m.common_docker_compose()}</h2>
						</div>
						<p class="text-muted-foreground mt-0.5 text-xs">{m.templates_service_definitions()}</p>
					</div>
					<div class="relative z-[var(--arcane-z-content)] flex min-h-0 min-w-0 flex-1 flex-col overflow-visible">
						<div class="absolute inset-0 min-h-0 w-full min-w-0">
							<CodeEditor bind:value={data.templateData.content} language="yaml" readOnly={true} fontSize="13px" />
						</div>
					</div>
				</div>

				<div class="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-6 lg:col-span-1">
					{#if services?.length}
						<DetailPanel class="min-w-0 flex-shrink-0">
							<DetailSectionCard icon={ContainersIcon} title={m.services()} description={m.templates_containers_to_create()}>
								<div class="divide-border/50 divide-y">
									{#each services as service (service)}
										<div class="flex min-w-0 items-center gap-2 py-2 first:pt-0 last:pb-0">
											<BoxIcon class="size-4 shrink-0 text-blue-500" />
											<div class="min-w-0 flex-1 truncate font-mono text-sm font-semibold">{service}</div>
										</div>
									{/each}
								</div>
							</DetailSectionCard>
						</DetailPanel>
					{/if}

					{#if envVars?.length}
						<DetailPanel class="min-w-0 flex-shrink-0">
							<DetailSectionCard
								icon={VariableIcon}
								title={m.common_environment_variables()}
								description={m.templates_default_config_values()}
							>
								<div class="grid grid-cols-1 gap-x-6 gap-y-4">
									{#each envVars as envVar (envVar.key)}
										<div class="min-w-0">
											<div class="text-muted-foreground text-[11px] font-semibold tracking-wide break-words uppercase select-all">
												{envVar.key}
											</div>
											{#if envVar.value}
												<div class="text-foreground mt-1 min-w-0 font-mono text-sm break-words select-all">{envVar.value}</div>
											{:else}
												<div class="text-muted-foreground mt-1 text-xs italic">{m.common_no_default_value()}</div>
											{/if}
										</div>
									{/each}
								</div>
							</DetailSectionCard>
						</DetailPanel>
					{/if}

					{#if data.templateData.envContent}
						<div class="border-border/70 flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl border">
							<div class="border-border/50 flex-shrink-0 border-b px-4 py-2.5">
								<div class="flex items-center gap-2">
									<FileTextIcon class="text-primary size-4 shrink-0" />
									<h2 class="text-sm font-semibold">{m.environment_file()}</h2>
								</div>
								<p class="text-muted-foreground mt-0.5 text-xs">{m.templates_raw_env_config()}</p>
							</div>
							<div class="relative z-[var(--arcane-z-content)] flex min-h-0 min-w-0 flex-1 flex-col overflow-visible">
								<div class="absolute inset-0 min-h-0 w-full min-w-0">
									<CodeEditor bind:value={data.templateData.envContent} language="env" readOnly={true} fontSize="13px" />
								</div>
							</div>
						</div>
					{/if}
				</div>
			</div>
		</div>
	{/if}
</div>
