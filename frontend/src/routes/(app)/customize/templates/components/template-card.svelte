<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import IconImage from '$lib/components/icon-image.svelte';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { goto } from '$app/navigation';
	import { m } from '$lib/paraglide/messages';
	import { hasPermission } from '$lib/utils/auth';
	import type { Template } from '$lib/types/swarm';
	import { InspectIcon, FolderOpenIcon, GlobeIcon, TrashIcon, DownloadIcon, MoveToFolderIcon } from '$lib/icons';

	let {
		template,
		downloading = false,
		deleting = false,
		onDownload,
		onDelete
	}: {
		template: Template;
		downloading?: boolean;
		deleting?: boolean;
		onDownload: (template: Template) => void;
		onDelete: (template: Template) => void;
	} = $props();

	const MAX_TAGS = 4;

	const canReadTemplate = $derived(hasPermission('templates:read'));
	const canDeleteTemplate = $derived(hasPermission('templates:delete'));
	const registryName = $derived(
		template.registry?.name ?? (template.isRemote ? m.templates_unknown_registry() : m.templates_local_templates())
	);
	const tags = $derived(template.metadata?.tags ?? []);
</script>

<div
	class="group relative flex h-full flex-col gap-3 rounded-xl border border-border/50 bg-card/30 p-4 transition-colors hover:bg-muted/40"
>
	<div class="flex items-start gap-3">
		<IconImage
			src={template.metadata?.iconUrl}
			alt={template.name}
			fallback={template.isRemote ? GlobeIcon : FolderOpenIcon}
			class="size-5"
			containerClass="size-9"
		/>
		<div class="min-w-0 flex-1">
			<a
				href="/customize/templates/{template.id}"
				class="block truncate font-medium after:absolute after:inset-0 hover:underline"
			>
				{template.name}
			</a>
			<p class="truncate text-xs text-muted-foreground">
				{template.isRemote ? m.templates_remote() : m.local()} · {registryName}
			</p>
		</div>
		<div class="relative z-10 -mt-1 -mr-1 shrink-0">
			<RowActionsMenu>
				<DropdownMenu.Item onclick={() => goto(`/customize/templates/${template.id}`)}>
					<InspectIcon class="size-4" />
					{m.common_view_details()}
				</DropdownMenu.Item>

				<IfPermitted perm="projects:create">
					<DropdownMenu.Item onclick={() => goto(`/projects/new?templateId=${template.id}`)}>
						<MoveToFolderIcon class="size-4" />
						{m.compose_create_project()}
					</DropdownMenu.Item>
				</IfPermitted>

				{#if (template.isRemote && canReadTemplate) || (!template.isRemote && canDeleteTemplate)}
					<DropdownMenu.Separator />
				{/if}

				{#if template.isRemote && canReadTemplate}
					<DropdownMenu.Item onclick={() => onDownload(template)} disabled={downloading}>
						{#if downloading}
							<Spinner class="size-4" />
						{:else}
							<DownloadIcon class="size-4" />
						{/if}
						{m.templates_download()}
					</DropdownMenu.Item>
				{:else if !template.isRemote && canDeleteTemplate}
					<DropdownMenu.Item variant="destructive" onclick={() => onDelete(template)} disabled={deleting}>
						{#if deleting}
							<Spinner class="size-4" />
						{:else}
							<TrashIcon class="size-4" />
						{/if}
						{m.templates_delete_template()}
					</DropdownMenu.Item>
				{/if}
			</RowActionsMenu>
		</div>
	</div>

	{#if template.description}
		<p class="line-clamp-3 text-sm text-muted-foreground">{template.description}</p>
	{:else}
		<p class="text-sm text-muted-foreground italic">{m.common_no_description()}</p>
	{/if}

	{#if tags.length > 0}
		<div class="mt-auto flex flex-wrap gap-1">
			{#each tags.slice(0, MAX_TAGS) as tag (tag)}
				<Badge variant="outline" class="text-xs">{tag}</Badge>
			{/each}
			{#if tags.length > MAX_TAGS}
				<Badge variant="outline" class="text-xs">+{tags.length - MAX_TAGS}</Badge>
			{/if}
		</div>
	{/if}
</div>
