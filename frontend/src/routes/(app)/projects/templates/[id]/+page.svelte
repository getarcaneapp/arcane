<script lang="ts">
	import { afterNavigate, goto, refreshAll } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { DetailMetaStrip, DetailSection, KeyValueCard } from '#lib/components/resource-detail/index.js';
	import { TabBar } from '#lib/components/tab-bar/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useHighlightedCode } from '#lib/hooks/use-highlighted-code.svelte.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import {
		EllipsisIcon,
		CodeIcon,
		GlobeIcon,
		ContainersIcon,
		FileTextIcon,
		RegistryIcon,
		ExternalLinkIcon,
		MoveToFolderIcon,
		TemplateIcon,
		TrashIcon
	} from '#lib/icons/index.js';
	import { ResourceDetailLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { templateService } from '#lib/services/template-service.js';
	import { hasShownPermission } from '#lib/utils/environments.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { createForm } from '#lib/utils/settings.svelte.js';
	import {
		createNamedTemplateSchema,
		getTemplateEditorSaveState,
		resetTemplateEditorFields,
		runTemplateEditorSave
	} from '#lib/utils/template-editor.js';
	import { globalVariablesToMap } from '#lib/utils/template-load.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import TemplateEditorWorkspace from '../components/template-editor-workspace.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let template = $derived(data.templateData.template);
	let services = $derived(data.templateData.services);
	let envVars = $derived(data.templateData.envVariables);

	const status = $state({
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

	const formSchema = createNamedTemplateSchema();

	let originalName = $derived(template.name);
	let originalDescription = $derived(template.description ?? '');
	let originalCompose = $derived(data.templateData.content);
	let originalEnv = $derived(data.templateData.envContent);

	function formValues() {
		return {
			name: originalName,
			description: originalDescription,
			composeContent: originalCompose,
			envContent: originalEnv
		};
	}

	const form = createForm<typeof formSchema>(formSchema, untrack(formValues));

	afterNavigate(() => form.reset(formValues()));

	const remoteComposeLines = useHighlightedCode(
		() => data.templateData.content,
		() => 'yaml'
	);
	const remoteEnvLines = useHighlightedCode(
		() => data.templateData.envContent ?? '',
		() => 'env'
	);

	const hasChanges = $derived(
		form.inputs.name.value !== originalName ||
			form.inputs.description.value !== originalDescription ||
			form.inputs.composeContent.value !== originalCompose ||
			form.inputs.envContent.value !== originalEnv
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
				templateService.updateTemplate(
					template.id,
					changedFields(
						{ name: originalName, description: originalDescription, content: originalCompose, envContent: originalEnv },
						{
							name: validated.name,
							description: validated.description,
							content: validated.composeContent,
							envContent: validated.envContent
						}
					)
				),
			failureMessage: m.templates_save_template_failed(),
			setLoading: (value) => (status.saving = value),
			onSuccess: async (validated) => {
				toast.success(m.templates_save_template_success({ name: validated.name }));
				originalName = validated.name;
				originalDescription = validated.description ?? '';
				originalCompose = validated.composeContent;
				originalEnv = validated.envContent ?? '';
				await Promise.all([
					queryClient.invalidateQueries({
						queryKey: queryKeys.templates.content(template.id)
					}),
					queryClient.invalidateQueries({
						queryKey: queryKeys.templates.all
					})
				]);
				await refreshAll();
			}
		});
	}

	function handleReset() {
		resetTemplateEditorFields([
			{
				set: (value) => (form.inputs.name.value = value),
				value: originalName
			},
			{
				set: (value) => (form.inputs.description.value = value),
				value: originalDescription
			},
			{
				set: (value) => (form.inputs.composeContent.value = value),
				value: originalCompose
			},
			{
				set: (value) => (form.inputs.envContent.value = value),
				value: originalEnv
			}
		]);
	}

	const localVersionOfRemote = $derived.by(() => {
		if (!template.isRemote || !template.metadata?.remoteUrl) return null;
		return data.allTemplates.find((t) => !t.isRemote && t.metadata?.remoteUrl === template.metadata?.remoteUrl);
	});

	const canDownload = $derived(template.isRemote && !localVersionOfRemote && hasShownPermission('templates:create'));

	async function handleDownload() {
		if (status.isDownloading || !canDownload) return;
		status.isDownloading = true;
		try {
			const operationResult = await tryCatch(
				(async () => {
					const downloadedTemplate = await templateService.download(template.id);
					toast.success(m.templates_downloaded_success({ name: template.name }));
					if (downloadedTemplate?.id) {
						await goto(resolve('/(app)/projects/templates/[id]', { id: downloadedTemplate.id }), { replace: true });
					} else {
						await Promise.all([
							queryClient.invalidateQueries({
								queryKey: queryKeys.templates.content(template.id)
							}),
							queryClient.invalidateQueries({
								queryKey: queryKeys.templates.all
							})
						]);
						await refreshAll();
					}
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;

				console.error('Error downloading template:', error);
				toast.error(error instanceof Error ? error.message : m.templates_download_failed());
			}
		} finally {
			status.isDownloading = false;
		}
	}

	async function handleDelete() {
		if (status.isDeleting) return;
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_template() }),
			message: m.common_delete_confirm({
				resource: `${m.resource_template()} "${template.name}"`
			}),
			confirm: {
				label: m.templates_delete_template(),
				destructive: true,
				action: async () => {
					status.isDeleting = true;
					const operationResult = await tryCatch(
						(async () => {
							await templateService.deleteTemplate(template.id);
							toast.success(
								m.common_delete_success({
									resource: `${m.resource_template()} "${template.name}"`
								})
							);
							await goto(resolve('/(app)/projects/templates'));
						})()
					);
					if (operationResult.error !== null) {
						const error = operationResult.error;

						console.error('Error deleting template:', error);
						toast.error(
							error instanceof Error
								? error.message
								: m.common_delete_failed({
										resource: `${m.resource_template()} "${template.name}"`
									})
						);
						status.isDeleting = false;
					}
				}
			}
		});
	}

	const remoteActions = $derived.by(() => {
		const actions: ActionButton[] = [
			{
				id: 'create-project',
				action: 'create',
				placement: 'primary',
				label: m.compose_create_project(),
				onclick: () => goto(resolve(`/(app)/projects/new?templateId=${encodeURIComponent(template.id)}`))
			}
		];
		if (canDownload) {
			actions.push({
				id: 'download',
				action: 'base',
				placement: 'secondary',
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
				placement: 'secondary',
				label: m.templates_view_local_version(),
				onclick: () => goto(resolve('/(app)/projects/templates/[id]', { id: localVersionOfRemote.id }))
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

	const urlTab = useUrlTab({
		tabs: () => [
			{ value: 'overview', label: m.common_overview(), icon: TemplateIcon },
			{ value: 'compose', label: 'compose.yaml', icon: CodeIcon },
			{ value: 'env', label: '.env', icon: FileTextIcon, visible: !!data.templateData.envContent }
		],
		defaultTab: () => 'overview',
		ready: () => template.isRemote
	});

	const metaItems = $derived(
		[
			{ label: m.common_version(), value: template.metadata?.version ?? '' },
			{ label: m.common_author(), value: template.metadata?.author ?? '' },
			{
				label: m.common_updated(),
				value: template.metadata?.updatedAt ? formatDateTimeShort(template.metadata.updatedAt) : ''
			}
		].filter((item) => item.value !== '')
	);
</script>

{#if !template.isRemote}
	<TemplateEditorWorkspace
		bind:inputs={form.inputs}
		bind:validation
		{originalName}
		{originalCompose}
		{originalEnv}
		fileIdPrefix="templates:custom:{template.id}"
		{globalVariableMap}
		saving={status.saving}
		lockNameAndDescription={data.templateData.isFolderTemplate}
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
						<DropdownMenu.Item onclick={() => goto(resolve(`/(app)/projects/new?templateId=${encodeURIComponent(template.id)}`))}>
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
	<ResourceDetailLayout
		backUrl="/projects/templates"
		backLabel={m.templates_title()}
		title={template.name}
		subtitle={template.description}
		actions={remoteActions}
	>
		{#snippet badges()}
			<Badge variant="secondary">
				<GlobeIcon class="size-3" />
				{m.templates_remote()}
			</Badge>
			{#if template.registry?.name}
				<Badge variant="outline">
					<RegistryIcon class="size-3" />
					{template.registry.name}
				</Badge>
			{/if}
		{/snippet}

		<Tabs.Root value={urlTab.value}>
			<div class="space-y-6">
				<TabBar items={urlTab.items} onValueChange={(value) => urlTab.select(value)} />

				<Tabs.Content value="overview">
					<div class="space-y-6">
						{#if metaItems.length > 0 || template.metadata?.documentationUrl || template.metadata?.tags?.length}
							<DetailMetaStrip items={metaItems}>
								{#if template.metadata?.documentationUrl}
									<div class="flex items-center gap-1.5 text-sm text-muted-foreground">
										<span class="text-xs font-semibold tracking-wide uppercase">{m.common_documentation()}</span>
										<a
											href={template.metadata.documentationUrl}
											target="_blank"
											rel="external noopener noreferrer"
											class="inline-flex items-center gap-1 font-medium break-all text-primary-text hover:underline"
										>
											{documentationHost}
											<ExternalLinkIcon class="size-3.5 shrink-0" />
										</a>
									</div>
								{/if}
								{#if template.metadata?.tags?.length}
									<div class="flex flex-wrap items-center gap-1.5 text-sm text-muted-foreground">
										<span class="text-xs font-semibold tracking-wide uppercase">{m.common_tags()}</span>
										{#each template.metadata.tags as tag (tag)}
											<Badge variant="outline">{tag}</Badge>
										{/each}
									</div>
								{/if}
							</DetailMetaStrip>
						{/if}

						{#if services?.length}
							<DetailSection title="{m.services()} ({services.length})" icon={ContainersIcon} class="border-t-0 pt-0">
								<ul class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
									{#each services as service (service)}
										<li class="flex min-w-0 items-center gap-2.5 rounded-xl border border-border/50 bg-card/10 px-3.5 py-3">
											<ContainersIcon class="size-4 shrink-0 text-muted-foreground" />
											<span class="min-w-0 truncate font-mono text-sm" title={service}>{service}</span>
										</li>
									{/each}
								</ul>
							</DetailSection>
						{/if}

						{#if envVars?.length}
							<DetailSection title="{m.common_environment_variables()} ({envVars.length})" icon={FileTextIcon}>
								<div class="grid grid-cols-1 gap-x-6 sm:grid-cols-2">
									{#each envVars as envVar (envVar.key)}
										{#if envVar.value}
											<KeyValueCard label={envVar.key} valueTitle={envVar.value}>{envVar.value}</KeyValueCard>
										{:else}
											<KeyValueCard label={envVar.key} valueFormat="custom">
												<span class="text-sm text-muted-foreground italic">{m.common_no_default_value()}</span>
											</KeyValueCard>
										{/if}
									{/each}
								</div>
							</DetailSection>
						{/if}
					</div>
				</Tabs.Content>

				<Tabs.Content value="compose">
					<div class="h-screen-inset-15 min-h-80">
						<CodeBlock.Root class="h-full">
							<CodeBlock.Actions>
								<CopyButton text={data.templateData.content} size="icon" class="size-7" />
							</CodeBlock.Actions>
							<CodeBlock.Content lineNumbers class="flex-1">
								{#each remoteComposeLines.current as line, index (index)}
									<CodeBlock.Line code={line} />
								{/each}
							</CodeBlock.Content>
						</CodeBlock.Root>
					</div>
				</Tabs.Content>

				{#if data.templateData.envContent}
					<Tabs.Content value="env">
						<div class="h-screen-inset-15 min-h-80">
							<CodeBlock.Root class="h-full">
								<CodeBlock.Actions>
									<CopyButton text={data.templateData.envContent} size="icon" class="size-7" />
								</CodeBlock.Actions>
								<CodeBlock.Content lineNumbers class="flex-1">
									{#each remoteEnvLines.current as line, index (index)}
										<CodeBlock.Line code={line} />
									{/each}
								</CodeBlock.Content>
							</CodeBlock.Root>
						</div>
					</Tabs.Content>
				{/if}
			</div>
		</Tabs.Root>
	</ResourceDetailLayout>
{/if}
