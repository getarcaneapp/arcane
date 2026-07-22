<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import TemplateEditorWorkspace from '../components/template-editor-workspace.svelte';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { goto, refreshAll } from '$app/navigation';
	import { m } from '$lib/paraglide/messages.js';
	import { templateService } from '$lib/services/template-service';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { createForm } from '$lib/utils/settings';
	import { formatDateTimeShort } from '$lib/utils/formatting';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import {
		createNamedTemplateSchema,
		getTemplateEditorSaveState,
		resetTemplateEditorFields,
		runTemplateEditorSave
	} from '$lib/utils/template-editor';
	import {
		EllipsisIcon,
		CodeIcon,
		GlobeIcon,
		BoxIcon,
		FileTextIcon,
		RegistryIcon,
		ExternalLinkIcon,
		MoveToFolderIcon,
		TrashIcon
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
	// fallow-ignore-next-line code-duplication -- template editor form wiring (createForm + getTemplateEditorSaveState); hasChanges fields differ per page
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
				await refreshAll();
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
				await refreshAll();
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

	const remoteActions = $derived.by(() => {
		const actions: DetailAction[] = [
			{
				id: 'create-project',
				action: 'create',
				label: m.compose_create_project(),
				onclick: () => goto(`/projects/new?templateId=${template.id}`)
			}
		];
		if (canDownload) {
			actions.push({
				id: 'download',
				action: 'base',
				label: m.templates_download(),
				loadingLabel: m.common_action_downloading(),
				loading: status.isDownloading,
				disabled: status.isDownloading,
				onclick: handleDownload
			});
		} else if (localVersionOfRemote) {
			actions.push({
				id: 'view-local',
				action: 'base',
				label: m.templates_view_local_version(),
				onclick: () => goto(`/customize/templates/${localVersionOfRemote?.id}`)
			});
		}
		return actions;
	});

	const documentationHost = $derived.by(() => {
		const url = template.metadata?.documentationUrl;
		if (!url) return null;
		try {
			return new URL(url).hostname;
		} catch {
			return url;
		}
	});
</script>

{#if !template.isRemote}
	<!-- Editor workspace for custom templates (same chrome as the create page) -->
	<TemplateEditorWorkspace
		{inputs}
		bind:validation
		{originalName}
		{originalCompose}
		{originalEnv}
		fileIdPrefix="templates:custom:{template.id}"
		{globalVariableMap}
		saving={status.saving}
		onSubmit={handleSave}
	>
		{#snippet toolbarActions()}
			<ArcaneButton action="cancel" onclick={handleReset} disabled={!hasChanges || status.saving}>
				{m.common_reset()}
			</ArcaneButton>
			<ArcaneButton
				action="save"
				onclick={handleSave}
				disabled={!canSave}
				loading={status.saving}
				loadingLabel={m.common_saving()}
			/>
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-9">
							<span class="sr-only">{m.common_open_menu()}</span>
							<EllipsisIcon class="size-4" />
						</ArcaneButton>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end">
					<IfPermitted perm="projects:create">
						<DropdownMenu.Item onclick={() => goto(`/projects/new?templateId=${template.id}`)}>
							<MoveToFolderIcon class="size-4" />
							{m.compose_create_project()}
						</DropdownMenu.Item>
						<DropdownMenu.Separator />
					</IfPermitted>
					<DropdownMenu.Item variant="destructive" onclick={handleDelete} disabled={status.isDeleting}>
						{#if status.isDeleting}
							<Spinner class="size-4" />
						{:else}
							<TrashIcon class="size-4" />
						{/if}
						{m.templates_delete_template()}
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{/snippet}
	</TemplateEditorWorkspace>
{:else}
	<!-- Marketplace-style read-only view for remote templates -->
	<ResourceDetailLayout
		backUrl="/customize/templates"
		backLabel={m.templates_title()}
		title={template.name}
		subtitle={template.description}
		actions={remoteActions}
	>
		{#snippet badges()}
			<Badge variant="secondary" class="gap-1">
				<GlobeIcon class="size-3" />
				{m.templates_remote()}
			</Badge>
			{#if template.registry?.name}
				<Badge variant="outline" class="gap-1">
					<RegistryIcon class="size-3" />
					{template.registry.name}
				</Badge>
			{/if}
		{/snippet}

		<div class="flex flex-col gap-8 lg:flex-row">
			<div class="min-w-0 flex-1 space-y-8">
				<section class="space-y-2">
					<div class="flex items-center gap-2">
						<CodeIcon class="size-4 text-muted-foreground" />
						<h2 class="font-mono text-sm font-medium">compose.yaml</h2>
					</div>
					<div class="relative h-[480px] overflow-hidden rounded-lg border border-border/50 sm:h-[560px]">
						<div class="absolute inset-0">
							<CodeEditor bind:value={data.templateData.content} language="yaml" readOnly={true} fontSize="13px" />
						</div>
					</div>
				</section>

				{#if data.templateData.envContent}
					<section class="space-y-2">
						<div class="flex items-center gap-2">
							<FileTextIcon class="size-4 text-muted-foreground" />
							<h2 class="font-mono text-sm font-medium">.env</h2>
						</div>
						<div class="relative h-[280px] overflow-hidden rounded-lg border border-border/50">
							<div class="absolute inset-0">
								<CodeEditor bind:value={data.templateData.envContent} language="env" readOnly={true} fontSize="13px" />
							</div>
						</div>
					</section>
				{/if}
			</div>

			<aside class="w-full shrink-0 space-y-6 lg:w-72 lg:border-l lg:border-border/50 lg:pl-8 xl:w-80">
				<div class="space-y-3">
					<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_overview()}</h3>
					<dl class="space-y-2 text-sm">
						{#if template.registry?.name}
							<div class="flex items-center justify-between gap-4">
								<dt class="shrink-0 text-muted-foreground">{m.common_registry()}</dt>
								<dd class="min-w-0 text-right font-medium break-words">{template.registry.name}</dd>
							</div>
						{/if}
						{#if template.metadata?.version}
							<div class="flex items-center justify-between gap-4">
								<dt class="shrink-0 text-muted-foreground">{m.common_version()}</dt>
								<dd class="min-w-0 text-right font-medium break-words">{template.metadata.version}</dd>
							</div>
						{/if}
						{#if template.metadata?.author}
							<div class="flex items-center justify-between gap-4">
								<dt class="shrink-0 text-muted-foreground">{m.common_author()}</dt>
								<dd class="min-w-0 text-right font-medium break-words">{template.metadata.author}</dd>
							</div>
						{/if}
						{#if template.metadata?.updatedAt}
							<div class="flex items-center justify-between gap-4">
								<dt class="shrink-0 text-muted-foreground">{m.common_updated()}</dt>
								<dd class="min-w-0 text-right font-medium break-words">{formatDateTimeShort(template.metadata.updatedAt)}</dd>
							</div>
						{/if}
						{#if template.metadata?.documentationUrl}
							<div class="flex items-center justify-between gap-4">
								<dt class="shrink-0 text-muted-foreground">{m.common_documentation()}</dt>
								<dd class="min-w-0 text-right">
									<a
										href={template.metadata.documentationUrl}
										target="_blank"
										rel="noopener noreferrer"
										class="inline-flex items-center gap-1 font-medium break-all text-primary hover:underline"
									>
										{documentationHost}
										<ExternalLinkIcon class="size-3.5 shrink-0" />
									</a>
								</dd>
							</div>
						{/if}
					</dl>
				</div>

				{#if services?.length}
					<div class="space-y-3 border-t border-border/50 pt-6">
						<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							{m.services()} ({services.length})
						</h3>
						<ul class="space-y-1.5">
							{#each services as service (service)}
								<li class="flex min-w-0 items-center gap-2 text-sm">
									<BoxIcon class="size-4 shrink-0 text-muted-foreground" />
									<span class="min-w-0 truncate font-mono">{service}</span>
								</li>
							{/each}
						</ul>
					</div>
				{/if}

				{#if envVars?.length}
					<div class="space-y-3 border-t border-border/50 pt-6">
						<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							{m.common_environment_variables()} ({envVars.length})
						</h3>
						<div class="space-y-2.5">
							{#each envVars as envVar (envVar.key)}
								<div class="min-w-0">
									<div class="font-mono text-xs font-medium break-words select-all">{envVar.key}</div>
									{#if envVar.value}
										<div class="font-mono text-xs break-words text-muted-foreground select-all">{envVar.value}</div>
									{:else}
										<div class="text-xs text-muted-foreground italic">{m.common_no_default_value()}</div>
									{/if}
								</div>
							{/each}
						</div>
					</div>
				{/if}

				{#if template.metadata?.tags && template.metadata.tags.length > 0}
					<div class="space-y-3 border-t border-border/50 pt-6">
						<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_tags()}</h3>
						<div class="flex flex-wrap gap-1">
							{#each template.metadata.tags as tag (tag)}
								<Badge variant="outline" class="text-xs">{tag}</Badge>
							{/each}
						</div>
					</div>
				{/if}
			</aside>
		</div>
	</ResourceDetailLayout>
{/if}
