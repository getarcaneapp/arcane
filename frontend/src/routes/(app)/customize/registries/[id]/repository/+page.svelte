<script lang="ts">
	import { toast } from 'svelte-sonner';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import type { BulkAction, ColumnSpec } from '#lib/components/arcane-table/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ResourceDetailLayout, type DetailAction } from '#lib/layouts/index.js';
	import { BoxIcon, CalendarIcon, LayersIcon, TagIcon, TrashIcon } from '#lib/icons/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerRegistryService } from '#lib/services/container-registry-service.js';
	import type { RegistryTag } from '#lib/types/docker.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { buildImageReference } from '#lib/utils/registry.js';

	let { data } = $props();

	const registry = $derived(data.registry);
	const repository = $derived(data.repository);
	const registryLabel = $derived(registry.url || 'docker.io');
	const registryHost = $derived(registryLabel.replace(/^https?:\/\//, '').split('/')[0] ?? registryLabel);
	let tags = $derived(data.tags);
	let requestOptions = $derived(data.requestOptions);
	let selectedIds = $state<string[]>([]);
	let deletingTag = $state<string | null>(null);
	let bulkDeleting = $state(false);
	let refreshing = $state(false);

	const canDeleteTags = $derived(hasPermission('registries:delete-tags'));

	function tagReference(tag: string) {
		return buildImageReference(registryHost, repository, tag);
	}

	function platformLabel(platform: RegistryTag['platforms'][number]) {
		return [platform.os, platform.architecture, platform.variant].filter(Boolean).join('/');
	}

	function shortDigest(digest?: string) {
		return digest ? digest.replace(/^(sha256:[a-f0-9]{12})[a-f0-9]+$/, '$1') : m.common_na();
	}

	async function loadTags(options = requestOptions) {
		const result = await tryCatch(containerRegistryService.getTags(registry.id, repository, options));
		if (result.error !== null) {
			toast.error(m.registries_tags_load_failed({ repository }), { description: extractApiErrorMessage(result.error) });
			return tags;
		}
		tags = result.data;
		return result.data;
	}

	async function refresh() {
		refreshing = true;
		await loadTags();
		refreshing = false;
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
				await loadTags();
			}
		});
	}

	function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;
		const selectedTags = [...ids];
		// Tags sharing a digest are deleted together, so a second request for the same digest would 404.
		const digestByTag = new Map(tags.data.map((tag) => [tag.name, tag.digest]));
		const seenDigests = new Set<string>();
		const tagsToDelete = selectedTags.filter((tag) => {
			const digest = digestByTag.get(tag);
			if (!digest) return true;
			if (seenDigests.has(digest)) return false;
			seenDigests.add(digest);
			return true;
		});

		bulkConfirmAndRun({
			ids: tagsToDelete,
			title: m.registries_delete_tags_selected_title({ count: selectedTags.length }),
			message: m.registries_delete_tags_selected_message(),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: (tag) => containerRegistryService.deleteTag(registry.id, repository, tag),
			messages: {
				success: () => m.registries_bulk_delete_tags_success({ count: selectedTags.length }),
				partial: (success, total, failed) => m.common_bulk_delete_partial({ success, total, failed, resource: m.common_tags() }),
				failure: () => m.registries_bulk_delete_tags_failed({ count: selectedTags.length })
			},
			setLoading: (loading) => (bulkDeleting = loading),
			onComplete: async ({ success }) => {
				if (success > 0) await loadTags();
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	const actions: DetailAction[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			label: m.common_refresh(),
			loading: refreshing,
			disabled: refreshing,
			onclick: refresh
		}
	]);

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'delete',
			label: m.registries_delete_tags_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: bulkDeleting,
			disabled: !canDeleteTags || bulkDeleting,
			icon: TrashIcon
		}
	]);

	const columns = [
		{
			accessorKey: 'name',
			title: m.common_name(),
			sortable: true,
			cell: NameCell
		},
		{
			accessorKey: 'digest',
			title: m.images_attestations_digest(),
			cell: DigestCell
		},
		{
			id: 'platforms',
			accessorFn: (row) => row.platforms.map(platformLabel).join(', '),
			title: m.platforms_label(),
			cell: PlatformsCell
		},
		{
			accessorKey: 'size',
			title: m.common_size(),
			cell: SizeCell
		},
		{
			accessorKey: 'created',
			title: m.common_created(),
			cell: CreatedCell
		}
	] satisfies ColumnSpec<RegistryTag>[];
</script>

{#snippet NameCell({ item }: { item: RegistryTag })}
	<div class="flex items-center gap-1">
		<span class="font-medium">{item.name}</span>
		<CopyButton text={tagReference(item.name)} class="size-6" title={m.registries_copy_reference()} />
	</div>
{/snippet}

{#snippet DigestCell({ item }: { item: RegistryTag })}
	{#if item.error}
		<span class="text-sm text-muted-foreground" title={item.error}>{m.registries_tag_details_unavailable()}</span>
	{:else}
		<span class="font-mono text-xs" title={item.digest}>{shortDigest(item.digest)}</span>
	{/if}
{/snippet}

{#snippet PlatformsCell({ item }: { item: RegistryTag })}
	<div class="flex flex-wrap gap-1">
		{#each item.platforms as platform (platform.digest)}
			<Badge variant="outline" class="font-mono text-xs">{platformLabel(platform)}</Badge>
		{/each}
	</div>
{/snippet}

{#snippet SizeCell({ item }: { item: RegistryTag })}
	<span class="text-sm">{item.error ? m.common_na() : bytes.format(item.size)}</span>
{/snippet}

{#snippet CreatedCell({ item }: { item: RegistryTag })}
	<span class="text-sm">{item.created ? formatDateTimeShort(item.created) : m.common_na()}</span>
{/snippet}

{#snippet TagMobileCard({ item }: { item: RegistryTag })}
	<UniversalMobileCard
		{item}
		icon={{ component: TagIcon, variant: 'purple' as const }}
		title={(item) => item.name}
		subtitle={(item) => (item.error ? m.registries_tag_details_unavailable() : shortDigest(item.digest))}
		fields={[
			{
				label: m.platforms_label(),
				getValue: (item: RegistryTag) => item.platforms.map(platformLabel).join(', '),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: item.platforms.length > 0
			},
			{
				label: m.common_size(),
				getValue: (item: RegistryTag) => bytes.format(item.size),
				icon: BoxIcon,
				iconVariant: 'gray' as const,
				show: !item.error
			},
			{
				label: m.common_created(),
				getValue: (item: RegistryTag) => (item.created ? formatDateTimeShort(item.created) : m.common_na()),
				icon: CalendarIcon,
				iconVariant: 'gray' as const,
				show: !!item.created
			}
		]}
		rowActions={canDeleteTags ? RowActions : undefined}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: RegistryTag })}
	<RowActionsMenu>
		<DropdownMenu.Item
			variant="destructive"
			onclick={() => handleDeleteOne(item.name)}
			disabled={!canDeleteTags || deletingTag === item.name}
		>
			{#if deletingTag === item.name}
				<Spinner class="size-4" />
			{:else}
				<TrashIcon class="size-4" />
			{/if}
			{m.common_delete()}
		</DropdownMenu.Item>
	</RowActionsMenu>
{/snippet}

<ResourceDetailLayout
	backUrl={`/customize/registries/${registry.id}`}
	backLabel={m.registries_repositories()}
	title={repository}
	subtitle={m.registries_repository_tags_subtitle({ repository, url: registryLabel })}
	{actions}
>
	<ArcaneTable
		persistKey="arcane-registry-tags-table"
		items={tags}
		bind:requestOptions
		bind:selectedIds
		selectionDisabled={!canDeleteTags}
		{bulkActions}
		onRefresh={loadTags}
		{columns}
		rowActions={canDeleteTags ? RowActions : undefined}
		mobileCard={TagMobileCard}
	/>
</ResourceDetailLayout>
