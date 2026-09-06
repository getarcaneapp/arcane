<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { KeyValueCard } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		ArrowDownIcon,
		ClockIcon,
		EllipsisIcon,
		InfoIcon,
		RedeployIcon,
		SettingsIcon,
		TagIcon,
		TerminalIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import type { ImageBuildRecord, ImageBuildStatus } from '#lib/types/docker.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import {
		buildHistoryStatusLabel,
		formatBuildDuration,
		formatBuildTimestamp,
		getBuildDetailItems,
		getBuildStatusBadgeVariant,
		getBuildTitle,
		parseBuildOutput
	} from '../image-build-history';

	interface Props {
		environmentId: string;
		onApplyBuild: (build: ImageBuildRecord) => void;
	}

	let { environmentId, onApplyBuild }: Props = $props();

	const EMPTY_BUILD_HISTORY: Paginated<ImageBuildRecord> = {
		data: [],
		pagination: { totalPages: 1, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	};

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	});
	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let selectedOptimistic = $state<ImageBuildRecord | null>(null);
	let selectedId = $state<string | null>(null);
	let detailsOpen = $state(false);

	const historyQuery = createQuery(() => ({
		queryKey: queryKeys.images.buildsList(environmentId, requestOptions),
		queryFn: () => imageService.getImageBuilds(requestOptions)
	}));

	const historyItems = $derived<Paginated<ImageBuildRecord>>(historyQuery.data ?? EMPTY_BUILD_HISTORY);

	let historyLastError: string | null = null;
	$effect(() => {
		const error = historyQuery.error;
		if (!error) return;
		const message = error instanceof Error ? error.message : m.common_error();
		if (message && message !== historyLastError) {
			historyLastError = message;
			toast.error(message);
		}
	});

	const detailQuery = createQuery(() => ({
		queryKey: selectedId
			? queryKeys.images.buildRecord(environmentId, selectedId)
			: (['images', environmentId, 'builds', 'none'] as const),
		queryFn: () => imageService.getImageBuild(selectedId!),
		enabled: !!selectedId && detailsOpen
	}));

	const selectedBuild = $derived<ImageBuildRecord | null>(detailQuery.data ?? selectedOptimistic);
	const detailsLoading = $derived(detailQuery.isPending || detailQuery.isFetching);
	const outputEntries = $derived.by(() => parseBuildOutput(selectedBuild?.output ?? ''));

	let detailLastError: string | null = null;
	$effect(() => {
		const error = detailQuery.error;
		if (!error) return;
		const message = error instanceof Error ? error.message : m.common_error();
		if (message && message !== detailLastError) {
			detailLastError = message;
			toast.error(message);
		}
	});

	const detailItems = $derived(selectedBuild ? getBuildDetailItems(selectedBuild) : []);

	const columns = [
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
	] satisfies ColumnSpec<ImageBuildRecord>[];

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'tags', label: m.common_tags(), defaultVisible: true },
		{ id: 'context', label: m.build_context(), defaultVisible: true },
		{ id: 'provider', label: m.build_provider(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'durationMs', label: m.duration(), defaultVisible: false }
	];

	async function loadHistory(options: SearchPaginationSortRequest = requestOptions) {
		requestOptions = options;
		const result = await historyQuery.refetch();
		return result.data ?? historyQuery.data ?? EMPTY_BUILD_HISTORY;
	}

	function openDetails(build: ImageBuildRecord) {
		selectedId = build.id;
		selectedOptimistic = build;
		detailsOpen = true;
	}

	function applySelectedBuild() {
		if (!selectedBuild) return;
		onApplyBuild(selectedBuild);
		detailsOpen = false;
	}
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

{#snippet BuildHistoryRowActions({ item }: { item: ImageBuildRecord })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger data-row-select-ignore>
			<ArcaneButton action="inspect" tone="ghost" size="icon" showLabel={false} icon={EllipsisIcon} />
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Item onclick={() => openDetails(item)}>
				<InfoIcon class="size-4" />
				{m.common_view_details()}
			</DropdownMenu.Item>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet BuildHistoryMobileCard({
	item,
	mobileFieldVisibility: fieldVisibility
}: {
	item: ImageBuildRecord;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: ImageBuildRecord) => ({
			component: TerminalIcon,
			variant:
				item.status === 'success' ? 'emerald' : item.status === 'failed' ? 'red' : item.status === 'running' ? 'blue' : 'gray'
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
		onclick={(item: ImageBuildRecord) => openDetails(item)}
	/>
{/snippet}

<div class="flex h-full flex-col p-6">
	<ArcaneTable
		persistKey="arcane-build-history-table"
		items={historyItems}
		bind:requestOptions
		bind:selectedIds
		bind:mobileFieldVisibility
		onRefresh={loadHistory}
		selectionDisabled={true}
		{columns}
		{mobileFields}
		rowActions={BuildHistoryRowActions}
		mobileCard={BuildHistoryMobileCard}
	/>

	<ResponsiveDialog
		bind:open={detailsOpen}
		title={selectedBuild ? (selectedBuild.tags?.[0] ?? m.build_output()) : m.build_output()}
		description={selectedBuild ? selectedBuild.contextDir : undefined}
		contentClass="sm:max-w-[1100px]"
		class="min-h-0 lg:overflow-hidden"
	>
		<div class="space-y-4 pb-4">
			{#if selectedBuild}
				<div class="flex flex-wrap items-center justify-between gap-3 text-sm">
					<div class="flex flex-wrap items-center gap-2">
						<Badge variant={getBuildStatusBadgeVariant(selectedBuild.status)} size="sm" minWidth="20"
							>{buildHistoryStatusLabel(selectedBuild.status)}</Badge
						>
						{#if selectedBuild.provider}
							<span class="text-muted-foreground">{selectedBuild.provider}</span>
						{/if}
						{#if selectedBuild.durationMs}
							<span class="text-muted-foreground">{formatBuildDuration(selectedBuild.durationMs)}</span>
						{/if}
						<span class="text-muted-foreground">{formatBuildTimestamp(selectedBuild.createdAt)}</span>
					</div>
					<IfPermitted perm="images:build">
						<ArcaneButton
							action="base"
							tone="outline"
							size="sm"
							icon={RedeployIcon}
							customLabel={m.build_rebuild()}
							onclick={applySelectedBuild}
						/>
					</IfPermitted>
				</div>
				{#if selectedBuild.errorMessage}
					<div class="rounded-lg border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
						{selectedBuild.errorMessage}
					</div>
				{/if}
			{/if}

			<div class="grid gap-4 lg:h-[70vh] lg:grid-cols-[360px_minmax(0,1fr)] lg:items-stretch">
				<div class="min-h-0 space-y-3 lg:overflow-auto lg:overscroll-contain lg:pr-1">
					{#each detailItems as detail (detail.label)}
						<KeyValueCard
							label={detail.label}
							variant="outlined"
							contentClass="flex flex-col gap-2 p-3"
							labelClass="text-[10px] font-semibold tracking-[0.12em] text-muted-foreground uppercase"
							valueClass="text-xs break-all whitespace-pre-wrap"
						>
							{detail.value}
						</KeyValueCard>
					{/each}
				</div>
				<div class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-border/60 bg-card/30">
					<div class="flex items-center justify-between border-b border-border/60 px-4 py-3">
						<div class="text-sm font-medium">{m.build_output()}</div>
						{#if selectedBuild?.outputTruncated}
							<span class="text-xs text-amber-400">{m.build_output_truncated()}</span>
						{/if}
					</div>
					<div class="max-h-[60vh] min-h-[260px] overflow-auto overscroll-contain p-4 lg:max-h-none lg:min-h-0 lg:flex-1">
						{#if detailsLoading}
							<div class="flex h-full items-center justify-center">
								<Spinner class="size-6 text-muted-foreground" />
							</div>
						{:else if selectedBuild?.output}
							{#if outputEntries.length > 0}
								<div class="rounded-lg border border-border/50 bg-zinc-950/40 p-3 font-mono text-xs leading-relaxed">
									{#each outputEntries as entry, entryIndex (entry.text + entryIndex)}
										<div class={`break-words whitespace-pre-wrap ${entry.isError ? 'text-destructive' : 'text-foreground'}`}>
											{entry.text}
										</div>
									{/each}
								</div>
							{:else}
								<div class="text-sm text-muted-foreground">{m.build_output_placeholder()}</div>
							{/if}
						{:else}
							<div class="text-sm text-muted-foreground">{m.build_output_placeholder()}</div>
						{/if}
					</div>
				</div>
			</div>
		</div>
	</ResponsiveDialog>
</div>
