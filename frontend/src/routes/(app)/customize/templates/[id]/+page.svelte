<script lang="ts">
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
	import { createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { z } from 'zod/v4';
	import {
		TrashIcon,
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
	let saving = $state(false);
	let isDeleting = $state(false);
	let isDownloading = $state(false);
	let composeHasErrors = $state(false);
	let envHasErrors = $state(false);
	let composeValidationReady = $state(false);
	let envValidationReady = $state(false);

	const globalVariableMap = $derived.by(() =>
		Object.fromEntries((data.globalVariables ?? []).map((item) => [item.key, item.value]))
	);

	// Form schema for custom template editing
	const formSchema = z.object({
		name: z.string().min(1, m.templates_template_name_required()),
		description: z.string().optional().default(''),
		composeContent: z.string().min(1, m.templates_content_required()),
		envContent: z.string().optional().default('')
	});

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

	const canSave = $derived(hasChanges && composeValidationReady && envValidationReady && !composeHasErrors && !envHasErrors);

	async function handleSave() {
		if (!composeValidationReady || !envValidationReady || composeHasErrors || envHasErrors) {
			toast.error(m.templates_validation_error());
			return;
		}
		const validated = form.validate();
		if (!validated) {
			toast.error(m.templates_validation_error());
			return;
		}

		handleApiResultWithCallbacks({
			result: await tryCatch(
				templateService.updateTemplate(template.id, {
					name: validated.name,
					description: validated.description,
					content: validated.composeContent,
					envContent: validated.envContent
				})
			),
			message: m.templates_save_template_failed(),
			setLoadingState: (value) => (saving = value),
			onSuccess: async () => {
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
		$inputs.name.value = originalName;
		$inputs.description.value = originalDescription;
		$inputs.composeContent.value = originalCompose;
		$inputs.envContent.value = originalEnv;
		toast.info(m.templates_reset_success());
	}

	// Read-only view helpers (remote templates)
	const localVersionOfRemote = $derived(() => {
		if (!template.isRemote || !template.metadata?.remoteUrl) return null;
		return data.allTemplates.find((t) => !t.isRemote && t.metadata?.remoteUrl === template.metadata?.remoteUrl);
	});

	const canDownload = $derived(template.isRemote && !localVersionOfRemote());

	async function handleDownload() {
		if (isDownloading || !canDownload) return;
		isDownloading = true;
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
			isDownloading = false;
		}
	}

	async function handleDelete() {
		if (isDeleting) return;
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_template() }),
			message: m.common_delete_confirm({ resource: `${m.resource_template()} "${template.name}"` }),
			confirm: {
				label: m.templates_delete_template(),
				destructive: true,
				action: async () => {
					isDeleting = true;
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
						isDeleting = false;
					}
				}
			}
		});
	}
</script>

<div class="mx-auto flex h-full min-h-0 w-full max-w-full flex-col gap-6 overflow-hidden p-2 pb-10 sm:p-6 sm:pb-10">
	<!-- Header -->
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
						disabled={saving}
					/>
					<FormInput
						input={$inputs.description}
						label={m.templates_template_description_label()}
						placeholder={m.templates_template_description_placeholder()}
						disabled={saving}
					/>
				</div>
				<div class="flex flex-col gap-2 sm:flex-row sm:items-start">
					<ArcaneButton
						action="create"
						onclick={() => goto(`/projects/new?templateId=${template.id}`)}
						customLabel={m.compose_create_project()}
						class="w-full gap-2 sm:w-auto"
					/>
					<ArcaneButton action="cancel" onclick={handleReset} disabled={!hasChanges || saving}>
						{m.common_reset()}
					</ArcaneButton>
					{#if composeValidationReady && envValidationReady && !composeHasErrors && !envHasErrors}
						<ArcaneButton
							action="save"
							onclick={handleSave}
							disabled={!canSave}
							loading={saving}
							loadingLabel={m.common_action_saving()}
						/>
					{/if}
					<ArcaneButton
						action="remove"
						onclick={handleDelete}
						disabled={isDeleting}
						loading={isDeleting}
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
					<h1 class="text-xl font-bold break-words sm:text-2xl">{template.name}</h1>
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
					{#each template.metadata.tags as tag}
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
						disabled={isDownloading}
						loading={isDownloading}
						loadingLabel={m.common_action_downloading()}
						class="w-full gap-2 sm:w-auto"
					>
						<DownloadIcon class="size-4" />
						{m.templates_download()}
					</ArcaneButton>
				{:else if template.isRemote && localVersionOfRemote()}
					<ArcaneButton
						action="base"
						onclick={() => goto(`/customize/templates/${localVersionOfRemote()?.id}`)}
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
		<div class="flex min-h-0 flex-1 flex-col gap-6 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch">
			<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-3">
				<Card.Header icon={CodeIcon} class="shrink-0">
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.templates_compose_template_label()}</h2>
						</Card.Title>
						<Card.Description>{m.templates_service_definitions()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
					<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
						<CodeEditor
							bind:value={$inputs.composeContent.value}
							language="yaml"
							readOnly={saving}
							fontSize="13px"
							bind:hasErrors={composeHasErrors}
							bind:validationReady={composeValidationReady}
							fileId="templates:custom:{template.id}:compose"
							originalValue={originalCompose}
							enableDiff={true}
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					</div>
				</Card.Content>
				{#if $inputs.composeContent.error}
					<Card.Footer class="pt-0">
						<p class="text-destructive text-xs font-medium">{$inputs.composeContent.error}</p>
					</Card.Footer>
				{/if}
			</Card.Root>

			<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-2">
				<Card.Header icon={VariableIcon} class="shrink-0">
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.templates_env_template_label()}</h2>
						</Card.Title>
						<Card.Description>{m.templates_default_config_values()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
					<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
						<CodeEditor
							bind:value={$inputs.envContent.value}
							language="env"
							readOnly={saving}
							fontSize="13px"
							bind:hasErrors={envHasErrors}
							bind:validationReady={envValidationReady}
							fileId="templates:custom:{template.id}:env"
							originalValue={originalEnv}
							enableDiff={true}
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					</div>
				</Card.Content>
				{#if $inputs.envContent.error}
					<Card.Footer class="pt-0">
						<p class="text-destructive text-xs font-medium">{$inputs.envContent.error}</p>
					</Card.Footer>
				{/if}
			</Card.Root>
		</div>
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
							<div class="text-2xl font-bold">{services?.length ?? 0}</div>
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
							<div class="text-2xl font-bold">{envVars?.length ?? 0}</div>
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
				<Card.Root class="flex h-full min-h-0 min-w-0 flex-col lg:col-span-1 xl:col-span-2">
					<Card.Header icon={CodeIcon} class="flex-shrink-0">
						<div class="flex flex-col space-y-1.5">
							<Card.Title>
								<h2>{m.common_docker_compose()}</h2>
							</Card.Title>
							<Card.Description>{m.templates_service_definitions()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="relative z-0 flex min-h-0 min-w-0 flex-1 flex-col overflow-visible p-0">
						<div class="absolute inset-0 min-h-0 w-full min-w-0 rounded-t-none rounded-b-xl">
							<CodeEditor bind:value={data.templateData.content} language="yaml" readOnly={true} fontSize="13px" />
						</div>
					</Card.Content>
				</Card.Root>

				<div class="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-6 lg:col-span-1">
					{#if services?.length}
						<Card.Root class="min-w-0 flex-shrink-0">
							<Card.Header icon={ContainersIcon}>
								<div class="flex flex-col space-y-1.5">
									<Card.Title>
										<h2>{m.services()}</h2>
									</Card.Title>
									<Card.Description>{m.templates_containers_to_create()}</Card.Description>
								</div>
							</Card.Header>
							<Card.Content class="grid grid-cols-1 gap-2 p-4">
								{#each services as service}
									<Card.Root variant="subtle" class="min-w-0">
										<Card.Content class="flex min-w-0 items-center gap-3 p-3">
											<div class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-blue-500/10">
												<BoxIcon class="size-4 text-blue-500" />
											</div>
											<div class="min-w-0 flex-1 truncate font-mono text-sm font-semibold">{service}</div>
										</Card.Content>
									</Card.Root>
								{/each}
							</Card.Content>
						</Card.Root>
					{/if}

					{#if envVars?.length}
						<Card.Root class="min-w-0 flex-shrink-0">
							<Card.Header icon={VariableIcon}>
								<div class="flex flex-col space-y-1.5">
									<Card.Title>
										<h2>{m.common_environment_variables()}</h2>
									</Card.Title>
									<Card.Description>{m.templates_default_config_values()}</Card.Description>
								</div>
							</Card.Header>
							<Card.Content class="grid grid-cols-1 gap-2 p-4">
								{#each envVars as envVar}
									<Card.Root variant="subtle" class="min-w-0">
										<Card.Content class="flex min-w-0 flex-col gap-2 p-3">
											<div class="text-muted-foreground text-xs font-semibold tracking-wide break-words uppercase select-all">
												{envVar.key}
											</div>
											{#if envVar.value}
												<div class="text-foreground min-w-0 font-mono text-sm break-words select-all">{envVar.value}</div>
											{:else}
												<div class="text-muted-foreground text-xs italic">{m.common_no_default_value()}</div>
											{/if}
										</Card.Content>
									</Card.Root>
								{/each}
							</Card.Content>
						</Card.Root>
					{/if}

					{#if data.templateData.envContent}
						<Card.Root class="flex h-full min-h-0 min-w-0 flex-1 flex-col">
							<Card.Header icon={FileTextIcon} class="flex-shrink-0">
								<div class="flex flex-col space-y-1.5">
									<Card.Title>
										<h2>{m.environment_file()}</h2>
									</Card.Title>
									<Card.Description>{m.templates_raw_env_config()}</Card.Description>
								</div>
							</Card.Header>
							<Card.Content class="relative z-0 flex min-h-0 min-w-0 flex-1 flex-col overflow-visible p-0">
								<div class="absolute inset-0 min-h-0 w-full min-w-0 rounded-b-xl">
									<CodeEditor bind:value={data.templateData.envContent} language="env" readOnly={true} fontSize="13px" />
								</div>
							</Card.Content>
						</Card.Root>
					{/if}
				</div>
			</div>
		</div>
	{/if}
</div>
