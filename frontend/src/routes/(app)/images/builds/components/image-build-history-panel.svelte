<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { EmptyState } from '#lib/components/states/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import {
		ArrowDownIcon,
		ClockIcon,
		EllipsisIcon,
		HammerIcon,
		InfoIcon,
		RedeployIcon,
		SettingsIcon,
		TagIcon,
		TerminalIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { imageBuildListQuery } from '#lib/query/images.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { ImageBuildRecord, ImageBuildStatus } from '#lib/types/image.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { type EnvironmentRow, readableEnvironments } from '#lib/utils/environments.js';

	import {
		buildHistoryStatusLabel,
		formatBuildDuration,
		formatBuildTimestamp,
		getBuildStatusBadgeVariant,
		getBuildTitle
	} from '../image-build-history';
	import ImageBuildDetailPanel from './image-build-detail-panel.svelte';

	type BuildRow = EnvironmentRow<ImageBuildRecord>;

	interface Props {
		onApplyBuild: (build: ImageBuildRecord, environmentId: string) => void;
	}

	let { onApplyBuild }: Props = $props();

	const queryClient = useQueryClient();
	const EMPTY_BUILD_HISTORY: Paginated<BuildRow> = {
		data: [],
		pagination: { totalPages: 1, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	};

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	});
	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let selectedOptimistic = $state<BuildRow | null>(null);
	let selectedId = $state<string | null>(null);

	const historyQuery = createQuery(() => {
		const scope = environmentStore.shownKey;
		return {
			...imageBuildListQuery(scope, readableEnvironments('images:list'), requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined)
		};
	});

	const historyItems = $derived<Paginated<BuildRow>>(historyQuery.data ?? EMPTY_BUILD_HISTORY);

	const detailQuery = createQuery(() => {
		const requestedEnvironmentId = selectedOptimistic?.environmentId ?? 'none';
		const buildId = selectedId;
		return {
			queryKey: queryKeys.images.buildRecord(requestedEnvironmentId, buildId ?? 'none'),
			queryFn: () => imageService.getImageBuild(buildId!, requestedEnvironmentId),
			enabled: !!buildId
		};
	});

	const selectedBuild = $derived<ImageBuildRecord | null>(detailQuery.data ?? selectedOptimistic);

	onMount(() => {
		const cache = queryClient.getQueryCache();
		let historyLastError = '';
		let detailLastError = '';
		function reportError(error: unknown, detail: boolean) {
			if (!error || (detail && !selectedId)) return;
			let message: string = m.common_error();
			if (error instanceof Error) message = error.message;
			if (detail) {
				if (message === detailLastError) return;
				detailLastError = message;
			} else {
				if (message === historyLastError) return;
				historyLastError = message;
			}
			toast.error(message);
		}
		reportError(historyQuery.error, false);
		reportError(detailQuery.error, true);
		return cache.subscribe((event) => {
			if (event.type !== 'updated' && event.type !== 'observerResultsUpdated') return;
			if (
				event.query ===
				cache.find({ queryKey: queryKeys.images.buildsList(environmentStore.shownKey, requestOptions), exact: true })
			) {
				reportError(event.query.state.error, false);
			} else if (
				selectedId &&
				event.query ===
					cache.find({
						queryKey: queryKeys.images.buildRecord(selectedOptimistic?.environmentId ?? 'none', selectedId),
						exact: true
					})
			) {
				reportError(event.query.state.error, true);
			}
		});
	});

	const columns = $derived([
		...environmentColumns<BuildRow>(),
		{
			accessorKey: 'status',
			title: m.common_status(),
			sortable: true,
			cell: BuildHistoryStatusCell
		},
		{
			id: 'tags',
			title: m.common_tags(),
			cell: BuildHistoryTagsCell
		},
		{
			accessorKey: 'provider',
			title: m.build_provider(),
			sortable: true,
			cell: BuildHistoryProviderCell
		},
		{
			accessorKey: 'createdAt',
			title: m.common_created(),
			sortable: true,
			cell: BuildHistoryTimeCell
		},
		{
			accessorKey: 'durationMs',
			title: m.duration(),
			cell: BuildHistoryDurationCell
		}
	] satisfies ColumnSpec<BuildRow>[]);

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'tags', label: m.common_tags(), defaultVisible: true },
		{ id: 'context', label: m.build_context(), defaultVisible: true },
		{ id: 'provider', label: m.build_provider(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'durationMs', label: m.duration(), defaultVisible: false }
	];

	function openDetails(build: BuildRow) {
		selectedId = build.id;
		selectedOptimistic = build;
	}

	function closeDetails() {
		selectedId = null;
		selectedOptimistic = null;
	}

	function applySelectedBuild() {
		if (!selectedBuild || !selectedOptimistic) return;
		onApplyBuild(selectedBuild, selectedOptimistic.environmentId);
	}

	const buildIconVariant: Record<string, 'emerald' | 'red' | 'blue'> = {
		success: 'emerald',
		failed: 'red',
		running: 'blue'
	};
</script>

{#snippet BuildHistoryStatusCell({ value }: { value: unknown })}
	<Badge variant={getBuildStatusBadgeVariant(value as ImageBuildStatus)} size="sm" minWidth="20"
		>{buildHistoryStatusLabel(value as ImageBuildStatus)}</Badge
	>
{/snippet}

{#snippet BuildHistoryTagsCell({ item }: { item: ImageBuildRecord })}
	<div class="space-y-1">
		<div class="text-sm font-medium">{getBuildTitle(item)}</div>
		<div class="truncate text-xs text-muted-foreground">{item.contextDir}</div>
	</div>
{/snippet}

{#snippet BuildHistoryProviderCell({ value }: { value: unknown })}
	<span class="text-sm">{String(value ?? '-') || '-'}</span>
{/snippet}

{#snippet BuildHistoryTimeCell({ value }: { value: unknown })}
	<span class="text-sm">{formatBuildTimestamp(String(value ?? ''))}</span>
{/snippet}

{#snippet BuildHistoryDurationCell({ value }: { value: unknown })}
	<span class="text-sm">{formatBuildDuration(Number(value ?? 0))}</span>
{/snippet}

{#snippet BuildHistoryRowActions({ item }: { item: BuildRow })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger data-row-select-ignore>
			<ArcaneButton action="inspect" tone="ghost" size="icon" showLabel={false} icon={EllipsisIcon} />
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Item onclick={() => openDetails(item)}>
				<InfoIcon class="size-4" />
				{m.common_view_details()}
			</DropdownMenu.Item>
			{#if hasPermission('images:build', item.environmentId)}
				<DropdownMenu.Item onclick={() => onApplyBuild(item, item.environmentId)}>
					<RedeployIcon class="size-4" />
					{m.build_rebuild()}
				</DropdownMenu.Item>
			{/if}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet BuildHistoryMobileCard({
	item,
	mobileFieldVisibility: fieldVisibility
}: {
	item: BuildRow;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: ImageBuildRecord) => ({
			component: TerminalIcon,
			variant: buildIconVariant[item.status] ?? 'gray'
		})}
		title={(item: ImageBuildRecord) => getBuildTitle(item)}
		subtitle={(item: ImageBuildRecord) => ((fieldVisibility['context'] ?? true) ? item.contextDir : null)}
		badges={[
			(item: ImageBuildRecord) => ({
				variant: getBuildStatusBadgeVariant(item.status),
				text: buildHistoryStatusLabel(item.status)
			})
		]}
		fields={[
			{
				label: m.common_tags(),
				getValue: (item: ImageBuildRecord) => item.tags?.join(', ') || '-',
				icon: TagIcon,
				iconVariant: 'gray' as const,
				show: fieldVisibility['tags'] ?? true
			},
			{
				label: m.build_provider(),
				getValue: (item: ImageBuildRecord) => item.provider || '-',
				icon: SettingsIcon,
				iconVariant: 'gray' as const,
				show: fieldVisibility['provider'] ?? true
			},
			{
				label: m.common_created(),
				getValue: (item: ImageBuildRecord) => formatBuildTimestamp(item.createdAt),
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: fieldVisibility['createdAt'] ?? true
			},
			{
				label: m.duration(),
				getValue: (item: ImageBuildRecord) => formatBuildDuration(item.durationMs ?? 0),
				icon: ArrowDownIcon,
				iconVariant: 'gray' as const,
				show: fieldVisibility['durationMs'] ?? false
			}
		]}
		onclick={(item: BuildRow) => openDetails(item)}
	/>
{/snippet}

<div class="flex flex-col gap-4 lg:flex-row lg:items-start">
	<div class="min-w-0 flex-1">
		<ArcaneTable
			persistKey="arcane-build-history-table"
			items={historyItems}
			bind:requestOptions
			bind:selectedIds
			bind:mobileFieldVisibility
			onRetry={() => historyQuery.refetch()}
			selectionDisabled={true}
			{columns}
			{mobileFields}
			rowActions={BuildHistoryRowActions}
			mobileCard={BuildHistoryMobileCard}
		>
			{#snippet emptyState()}
				<EmptyState
					icon={HammerIcon}
					title={m.image_build_history_empty_title()}
					description={m.image_build_history_empty_description()}
				/>
			{/snippet}
		</ArcaneTable>
	</div>

	{#if selectedBuild && selectedOptimistic}
		<div class="border-t border-border/50 pt-4 lg:w-96 lg:shrink-0 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-4">
			<ImageBuildDetailPanel
				build={selectedBuild}
				environmentId={selectedOptimistic.environmentId}
				loading={detailQuery.isPending || detailQuery.isFetching}
				onClose={closeDetails}
				onRebuild={applySelectedBuild}
			/>
		</div>
	{/if}
</div>
