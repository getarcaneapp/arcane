<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import CodeEditor from '$lib/components/monaco-code-editor/editor.svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { m } from '$lib/paraglide/messages.js';
	import { templateService } from '$lib/services/template-service';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import {
		TrashIcon,
		ArrowLeftIcon,
		ProjectsIcon,
		FileTextIcon,
		CodeIcon,
		TemplateIcon,
		DownloadIcon,
		GlobeIcon,
		ContainersIcon,
		BoxIcon,
		VariableIcon,
		MoveToFolderIcon
	} from '$lib/icons';

	let { data } = $props();

	let template = $derived(data.templateData.template);
	let compose = $state(untrack(() => data.templateData.content));
	let env = $state(untrack(() => data.templateData.envContent));
	let services = $derived(data.templateData.services);
	let envVars = $derived(data.templateData.envVariables);

	let isDownloading = $state(false);
	let isDeleting = $state(false);

	const localVersionOfRemote = $derived(() => {
		if (!template.isRemote || !template.metadata?.remoteUrl) return null;
		return data.allTemplates.find((t) => !t.isRemote && t.metadata?.remoteUrl === template.metadata?.remoteUrl);
	});

	const canDelete = $derived(!template.isRemote);
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
		if (isDeleting || !canDelete) return;

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

<div class="mx-auto flex h-full min-h-0 w-full max-w-full flex-col gap-6 p-2 sm:p-6">
	<div class="flex-shrink-0 space-y-3 sm:space-y-4">
		<ArcaneButton action="base" tone="ghost" onclick={() => goto('/customize/templates')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.templates_title() })}</span>
		</ArcaneButton>

		<div>
			<h1 class="text-xl font-bold break-words sm:text-2xl">{template.name}</h1>
			{#if template.description}
				<p class="text-muted-foreground mt-1.5 text-sm break-words sm:text-base">{template.description}</p>
			{/if}
		</div>

		<div class="flex flex-wrap items-center gap-2">
			{#if template.isRemote}
				<Badge variant="secondary" class="gap-1">
					<GlobeIcon class="size-3" />
					{m.templates_remote()}
				</Badge>
			{:else}
				<Badge variant="secondary" class="gap-1">
					<TemplateIcon class="size-3" />
					{m.templates_local()}
				</Badge>
			{/if}
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

			{#if canDelete}
				<ArcaneButton
					action="remove"
					onclick={handleDelete}
					disabled={isDeleting}
					loading={isDeleting}
					loadingLabel={m.common_action_deleting()}
					customLabel={m.templates_delete_template()}
					class="w-full gap-2 sm:w-auto"
				/>
			{/if}
		</div>
	</div>

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
						<CodeEditor bind:value={compose} language="yaml" readOnly={true} fontSize="13px" />
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

				{#if env}
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
								<CodeEditor bind:value={env} language="env" readOnly={true} fontSize="13px" />
							</div>
						</Card.Content>
					</Card.Root>
				{/if}
			</div>
		</div>
	</div>
</div>
