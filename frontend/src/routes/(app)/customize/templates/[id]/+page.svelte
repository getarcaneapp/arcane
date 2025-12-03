<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import FileTextIcon from '@lucide/svelte/icons/file-text';
	import CodeIcon from '@lucide/svelte/icons/code';
	import BoxIcon from '@lucide/svelte/icons/box';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { goto, invalidateAll } from '$app/navigation';
	import { m } from '$lib/paraglide/messages.js';
	import { templateService } from '$lib/services/template-service';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

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

<div class="container mx-auto max-w-full space-y-6 overflow-hidden p-2 sm:p-6">
	<div class="space-y-3 sm:space-y-4">
		<Button variant="ghost" onclick={() => goto('/customize/templates')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.templates_title() })}</span>
		</Button>

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
					<LayersIcon class="size-3" />
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
			<Button onclick={() => goto(`/projects/new?templateId=${template.id}`)} class="w-full gap-2 sm:w-auto">
				<FolderIcon class="size-4" />
				{m.compose_create_project()}
			</Button>

			{#if canDownload}
				<Button variant="secondary" onclick={handleDownload} disabled={isDownloading} class="w-full gap-2 sm:w-auto">
					{#if isDownloading}
						<Spinner class="size-4" />
						{m.common_action_downloading()}
					{:else}
						<DownloadIcon class="size-4" />
						{m.templates_download()}
					{/if}
				</Button>
			{:else if template.isRemote && localVersionOfRemote()}
				<Button
					variant="outline"
					onclick={() => goto(`/customize/templates/${localVersionOfRemote()?.id}`)}
					class="w-full gap-2 sm:w-auto"
				>
					<FolderIcon class="size-4" />
					{m.templates_view_local_version()}
				</Button>
			{/if}

			{#if canDelete}
				<Button variant="destructive" onclick={handleDelete} disabled={isDeleting} class="w-full gap-2 sm:w-auto">
					{#if isDeleting}
						<Spinner class="size-4" />
						{m.common_action_deleting()}
					{:else}
						<Trash2Icon class="size-4" />
						{m.templates_delete_template()}
					{/if}
				</Button>
			{/if}
		</div>
	</div>

	<div class="grid gap-4 sm:grid-cols-2">
		<Card.Root variant="subtle">
			<Card.Content class="flex items-center gap-4 p-4">
				<div class="flex size-12 shrink-0 items-center justify-center rounded-lg bg-blue-500/10">
					<BoxIcon class="size-6 text-blue-500" />
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
					<FileTextIcon class="size-6 text-purple-500" />
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

	<div class="grid gap-6 lg:grid-cols-2 xl:grid-cols-3">
		<Card.Root class="flex min-w-0 flex-col lg:col-span-1 xl:col-span-2">
			<Card.Header icon={CodeIcon} class="flex-shrink-0">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.common_docker_compose()}</h2>
					</Card.Title>
					<Card.Description>{m.templates_service_definitions()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="min-h-[500px] flex-grow p-0 lg:h-full">
				<div class="h-full rounded-t-none rounded-b-xl [&_.cm-content]:text-xs sm:[&_.cm-content]:text-sm">
					<CodeEditor bind:value={compose} language="yaml" readOnly={true} />
				</div>
			</Card.Content>
		</Card.Root>

		<div class="flex min-w-0 flex-col gap-6 lg:col-span-1">
			{#if services?.length}
				<Card.Root class="min-w-0 flex-shrink-0">
					<Card.Header icon={BoxIcon}>
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
					<Card.Header icon={FileTextIcon}>
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
									<div class="text-muted-foreground truncate text-xs font-semibold tracking-wide uppercase">{envVar.key}</div>
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
				<Card.Root class="flex min-w-0 flex-grow flex-col lg:h-full">
					<Card.Header icon={FileTextIcon} class="flex-shrink-0">
						<div class="flex flex-col space-y-1.5">
							<Card.Title>
								<h2>{m.environment_file()}</h2>
							</Card.Title>
							<Card.Description>{m.templates_raw_env_config()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="h-[500px] flex-grow p-0 lg:h-full">
						<div class="h-full rounded-b-xl [&_.cm-content]:text-xs sm:[&_.cm-content]:text-sm">
							<CodeEditor bind:value={env} language="env" readOnly={true} />
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	</div>
</div>
