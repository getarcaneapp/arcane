<script lang="ts">
	import { goto, refreshAll } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Breadcrumb from '#lib/components/ui/breadcrumb/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertTriangleIcon, ArrowRightIcon, RegistryIcon, SearchIcon, TagIcon, TrashIcon } from '#lib/icons/index.js';
	import { ResourceDetailLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerRegistryService } from '#lib/services/container-registry-service.js';
	import type { RegistryTag } from '#lib/types/docker.js';
	import { cn } from '#lib/utils.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { bytes, formatRelativeTime } from '#lib/utils/formatting.js';
	import { buildImageReference, splitRegistryUrl } from '#lib/utils/registry.js';
	import { debounced } from '#lib/utils/ws.js';

	let { data } = $props();

	const queryClient = useQueryClient();

	const registry = $derived(data.registry);
	const registryLabel = $derived(registry.url || 'docker.io');
	// Repository names already carry any namespace from the registry URL.
	const { host: registryHost, namespace: registryNamespace } = $derived(splitRegistryUrl(registry.url));
	const repository = $derived(data.repository);
	const tags = $derived(data.tags);
	const canDeleteTags = $derived(hasPermission('registries:delete-tags'));

	let selecting = $state(false);
	let selected = $state<string[]>([]);
	let deletingTag = $state<string | null>(null);
	let bulkDeleting = $state(false);
	let refreshing = $state(false);

	// Configured names are relative to the registry URL namespace, like the build flow.
	const configuredNames = $derived(
		(registry.repositoryNames ?? []).map((name) => (registryNamespace ? `${registryNamespace}/${name}` : name))
	);
	const repositories = $derived(data.catalog?.data.map((item) => item.name) ?? configuredNames);

	function navigate(patch: { repository?: string; search?: string; page?: number }) {
		const params = new URLSearchParams();
		const target = patch.repository ?? repository;
		if (target) params.set('repository', target);
		const search = patch.search ?? data.search;
		if (search) params.set('search', search);
		if ((patch.page ?? 1) > 1) params.set('page', String(patch.page));
		selecting = false;
		selected = [];
		goto(`/customize/registries/${registry.id}?${params}`, { reset: false });
	}

	const search = debounced((value: string) => navigate({ search: value.trim(), page: 1 }), 300);

	function submitRepositoryQuery(event: SubmitEvent) {
		event.preventDefault();
		const value = String(new FormData(event.currentTarget as HTMLFormElement).get('query') ?? '')
			.trim()
			.replace(/^\/+|\/+$/g, '');
		if (data.catalog) {
			navigate({ search: value, page: 1 });
		} else if (value) {
			navigate({ repository: value, search: '', page: 1 });
		}
	}

	function tagReference(tag: string) {
		return buildImageReference(registryHost, repository, tag);
	}

	function platformLabel(platform: RegistryTag['platforms'][number]) {
		return [platform.os, platform.architecture, platform.variant].filter(Boolean).join('/');
	}

	function tagMeta(tag: RegistryTag) {
		if (tag.error) return m.registries_tag_details_unavailable();
		const parts = [bytes.format(tag.size)];
		if (tag.created) parts.push(formatRelativeTime(tag.created));
		return parts.join(' · ');
	}

	function toggleSelected(tag: string, checked: boolean) {
		if (checked) {
			selected = [...selected, tag];
		} else {
			selected = selected.filter((name) => name !== tag);
		}
	}

	// Loader queries outlive their stale windows only until invalidated, so
	// drop every cached page and search variant before rerunning loaders.
	async function refresh() {
		refreshing = true;
		try {
			const registries = queryKeys.containerRegistries;
			await queryClient.invalidateQueries({ queryKey: registries.repositoriesPrefix(registry.id), refetchType: 'none' });
			if (repository) {
				await queryClient.invalidateQueries({ queryKey: registries.tagsPrefix(registry.id, repository), refetchType: 'none' });
			}
			await refreshAll();
		} catch (err) {
			toast.error(m.common_refresh_failed({ resource: m.common_registry() }), { description: extractApiErrorMessage(err) });
		} finally {
			refreshing = false;
		}
	}

	function handleDeleteOne(tag: string) {
		const reference = tagReference(tag);
		confirmAndRun({
			title: m.registries_delete_tag_title({ tag }),
			message: m.registries_delete_tag_message({ reference }),
			confirmLabel: m.common_delete(),
			destructive: true,
			setLoading: (loading) => (deletingTag = loading ? tag : null),
			run: () => containerRegistryService.deleteTag(registry.id, repository, tag),
			failureMessage: m.registries_delete_tag_failed({ reference }),
			onSuccess: async () => {
				toast.success(m.registries_delete_tag_success({ reference }));
				await refresh();
			}
		});
	}

	function handleDeleteSelected() {
		const ids = selected;
		if (!ids.length) return;
		// Tags sharing a digest are deleted together, so a second request for the same digest would 404.
		const digestByTag = new Map(tags.data.map((tag) => [tag.name, tag.digest]));
		const seenDigests = new Set<string>();
		const tagsToDelete = ids.filter((tag) => {
			const digest = digestByTag.get(tag);
			if (!digest) return true;
			if (seenDigests.has(digest)) return false;
			seenDigests.add(digest);
			return true;
		});

		bulkConfirmAndRun({
			ids: tagsToDelete,
			title: m.registries_delete_tags_selected_title({ count: ids.length }),
			message: m.registries_delete_tags_selected_message(),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: (tag) => containerRegistryService.deleteTag(registry.id, repository, tag),
			messages: {
				success: () => m.registries_bulk_delete_tags_success({ count: ids.length }),
				partial: (success, total, failed) => m.common_bulk_delete_partial({ success, total, failed, resource: m.common_tags() }),
				failure: () => m.registries_bulk_delete_tags_failed({ count: ids.length })
			},
			setLoading: (loading) => (bulkDeleting = loading),
			onComplete: async ({ success }) => {
				if (success > 0) await refresh();
			},
			clearSelection: () => {
				selected = [];
				selecting = false;
			},
			sequential: true
		});
	}

	const actions: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			loading: refreshing,
			disabled: refreshing,
			onclick: refresh
		}
	]);
</script>

{#snippet repositoryGrid()}
	<div class="space-y-4">
		<form class="flex flex-wrap items-center gap-2" onsubmit={submitRepositoryQuery}>
			<InputGroup.Root class="min-w-0 flex-1 md:w-80 md:flex-none">
				<InputGroup.Addon>
					<SearchIcon aria-hidden="true" />
				</InputGroup.Addon>
				{#if data.catalog}
					<InputGroup.Input
						name="query"
						value={data.search}
						placeholder={m.common_search()}
						oninput={(e) => search(e.currentTarget.value)}
					/>
				{:else}
					<InputGroup.Input
						name="query"
						placeholder={m.registries_open_repository_placeholder({ namespace: registryNamespace })}
					/>
				{/if}
			</InputGroup.Root>
			{#if data.catalogError}
				<ArcaneButton action="inspect" type="submit" customLabel={m.registries_open_repository()} />
			{/if}
		</form>

		{#if data.catalogError && repositories.length > 0}
			<p class="text-sm text-muted-foreground">{m.registries_catalog_unavailable_configured()}</p>
		{/if}

		{#if repositories.length > 0}
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
				{#each repositories as name (name)}
					<button
						type="button"
						class="group flex items-center gap-3 rounded-xl border border-border/50 bg-card/30 p-4 text-left transition-colors hover:bg-muted/40"
						onclick={() => navigate({ repository: name, search: '', page: 1 })}
					>
						<span
							class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20 ring-inset group-hover:bg-primary/15"
						>
							<RegistryIcon class="size-5" />
						</span>
						<span class="min-w-0 flex-1">
							<span class="block truncate font-medium">{name}</span>
							<span class="block truncate text-xs text-muted-foreground">{registryHost}/{name}</span>
						</span>
						<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
					</button>
				{/each}
			</div>
			{#if data.catalog && data.catalog.pagination.totalItems > repositories.length}
				<p class="text-xs text-muted-foreground">
					{m.common_showing_of_total({ shown: repositories.length, total: data.catalog.pagination.totalItems })}
				</p>
			{/if}
		{:else if data.catalog}
			<EmptyState icon={SearchIcon} title={m.common_no_results_found()} description={m.common_no_results_hint()} />
		{:else if data.catalogError}
			<EmptyState
				icon={RegistryIcon}
				title={m.registries_catalog_unavailable_title()}
				description={m.registries_catalog_unavailable_description()}
			>
				<details class="w-full max-w-2xl rounded-lg border border-border/60 bg-muted/15 text-left">
					<summary class="cursor-pointer px-4 py-2 text-sm font-medium select-none">{m.common_error_details()}</summary>
					<p class="border-t px-4 py-3 font-mono text-xs break-all text-muted-foreground">{data.catalogError}</p>
				</details>
			</EmptyState>
		{/if}
	</div>
{/snippet}

{#snippet tagList()}
	<div class="space-y-4">
		<div class="flex flex-wrap items-center gap-3">
			<Breadcrumb.Root class="min-w-0 flex-1">
				<Breadcrumb.List>
					<Breadcrumb.Item>
						<Breadcrumb.Link onclick={() => navigate({ repository: '', search: '', page: 1 })} class="cursor-pointer">
							{m.registries_repositories()}
						</Breadcrumb.Link>
					</Breadcrumb.Item>
					<Breadcrumb.Separator />
					<Breadcrumb.Item>
						<Breadcrumb.Page><span class="font-mono">{repository}</span></Breadcrumb.Page>
					</Breadcrumb.Item>
				</Breadcrumb.List>
			</Breadcrumb.Root>
			<CopyButton text={`${registryHost}/${repository}`} class="size-7" title={m.registries_copy_reference()} />
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<InputGroup.Root class="min-w-0 flex-1 md:w-80 md:flex-none">
				<InputGroup.Addon>
					<SearchIcon aria-hidden="true" />
				</InputGroup.Addon>
				<InputGroup.Input value={data.search} placeholder={m.common_search()} oninput={(e) => search(e.currentTarget.value)} />
			</InputGroup.Root>
			{#if canDeleteTags && tags.data.length > 0}
				<div class="ml-auto flex items-center gap-2">
					{#if selecting}
						<ArcaneButton
							action="remove"
							size="sm"
							customLabel={m.registries_delete_tags_selected_count({ count: selected.length })}
							disabled={selected.length === 0 || bulkDeleting}
							loading={bulkDeleting}
							onclick={handleDeleteSelected}
						/>
						<ArcaneButton
							action="cancel"
							size="sm"
							onclick={() => {
								selecting = false;
								selected = [];
							}}
						/>
					{:else}
						<ArcaneButton action="base" size="sm" customLabel={m.common_select()} onclick={() => (selecting = true)} />
					{/if}
				</div>
			{/if}
		</div>

		{#if data.tagsError}
			<Alert.Root variant="destructive">
				<AlertTriangleIcon class="size-4" />
				<Alert.Title>{m.registries_tags_load_failed({ repository })}</Alert.Title>
				<Alert.Description>{data.tagsError}</Alert.Description>
			</Alert.Root>
		{:else if tags.data.length === 0}
			<EmptyState icon={TagIcon} title={m.common_no_results_found()} description={m.common_no_results_hint()} />
		{:else}
			<div class="divide-y divide-border/60 overflow-hidden rounded-lg border border-border/60">
				{#each tags.data as tag (tag.name)}
					<div
						class={cn(
							'group flex flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-3 transition-colors hover:bg-muted/40 lg:flex-nowrap',
							selected.includes(tag.name) && 'bg-muted/40'
						)}
					>
						{#if selecting}
							<Checkbox
								checked={selected.includes(tag.name)}
								onCheckedChange={(checked) => toggleSelected(tag.name, checked === true)}
								aria-label={m.common_select_row()}
							/>
						{:else}
							<TagIcon class="size-4 shrink-0 text-muted-foreground" />
						{/if}
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-1">
								<span class="truncate font-medium">{tag.name}</span>
								<span class="opacity-0 group-hover:opacity-100 focus-within:opacity-100">
									<CopyButton text={tagReference(tag.name)} class="size-6" title={m.registries_copy_reference()} />
								</span>
							</div>
							<p class="truncate text-xs text-muted-foreground" title={tag.error}>{tagMeta(tag)}</p>
						</div>
						{#if tag.digest}
							<div class="order-2 flex w-full min-w-0 items-center gap-1 lg:order-none lg:w-auto lg:flex-1">
								<code class="truncate font-mono text-xs text-muted-foreground" title={tag.digest}>{tag.digest}</code>
								<CopyButton text={tag.digest} class="size-6 shrink-0" title={m.images_attestations_digest()} />
							</div>
						{/if}
						{#if tag.platforms.length > 0}
							<div class="order-3 flex w-full flex-wrap gap-1 lg:order-none lg:w-auto lg:justify-end">
								{#each tag.platforms as platform (platform.digest)}
									<Badge variant="outline">{platformLabel(platform)}</Badge>
								{/each}
							</div>
						{/if}
						{#if canDeleteTags && !selecting}
							<RowActionsMenu
								triggerClass="order-1 size-8 shrink-0 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100 lg:order-none"
							>
								<DropdownMenu.Item
									variant="destructive"
									onclick={() => handleDeleteOne(tag.name)}
									disabled={deletingTag === tag.name}
								>
									{#if deletingTag === tag.name}
										<Spinner class="size-4" />
									{:else}
										<TrashIcon class="size-4" />
									{/if}
									{m.common_delete()}
								</DropdownMenu.Item>
							</RowActionsMenu>
						{/if}
					</div>
				{/each}
			</div>

			{#if tags.pagination.totalPages > 1}
				<div class="flex items-center justify-between text-sm text-muted-foreground">
					<span>{m.common_page_of({ page: tags.pagination.currentPage, total: tags.pagination.totalPages })}</span>
					<div class="flex gap-2">
						<ArcaneButton
							action="base"
							size="sm"
							customLabel={m.common_previous()}
							disabled={tags.pagination.currentPage <= 1}
							onclick={() => navigate({ page: tags.pagination.currentPage - 1 })}
						/>
						<ArcaneButton
							action="base"
							size="sm"
							customLabel={m.common_next()}
							disabled={tags.pagination.currentPage >= tags.pagination.totalPages}
							onclick={() => navigate({ page: tags.pagination.currentPage + 1 })}
						/>
					</div>
				</div>
			{/if}
		{/if}
	</div>
{/snippet}

<ResourceDetailLayout backUrl="/customize/registries" backLabel={m.registries_title()} title={registryLabel} {actions}>
	{#if repository}
		{@render tagList()}
	{:else}
		{@render repositoryGrid()}
	{/if}
</ResourceDetailLayout>
