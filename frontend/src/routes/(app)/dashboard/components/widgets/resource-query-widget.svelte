<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery, keepPreviousData, skipToken, useQueryClient } from '@tanstack/svelte-query';

	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import * as StatStrip from '#lib/components/stat-strip/index.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerListQuery } from '#lib/query/containers.js';
	import { imageListQuery } from '#lib/query/images.js';
	import { networkListQuery } from '#lib/query/networks.js';
	import { projectListQuery } from '#lib/query/projects.js';
	import { volumeListQuery } from '#lib/query/volumes.js';
	import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { DashboardResourceKind } from '#lib/types/auth.js';
	import type { PaginationResponse, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import type { DashboardWidget } from '#lib/utils/dashboard-layout.js';
	import { getStatusVariant } from '#lib/utils/docker.js';
	import type { EnvironmentFailure, EnvironmentRow } from '#lib/utils/environments.js';
	import { capitalizeFirstLetter } from '#lib/utils/formatting.js';

	import {
		getContainerDisplayName,
		getContainerStatusLabel,
		getStateBadgeVariant
	} from '../../../containers/container-table.helpers';
	import { shouldLoadEnvironment } from '../../overview.helpers';
	import DashboardPanel from '../dashboard-panel.svelte';
	import { DEFAULT_RESOURCE_LIMIT, RESOURCE_KINDS } from './resource-kinds';

	type ResourceRow = { key: string; name: string; status: string; variant: BadgeVariant; href: string; environmentId: string };
	type ResourceResult = { total: number; rows: ResourceRow[]; failedEnvironments: EnvironmentFailure[] };
	type ResourcePage<T> = { data: EnvironmentRow<T>[]; pagination: PaginationResponse; failedEnvironments?: EnvironmentFailure[] };

	let { widget }: { widget: DashboardWidget } = $props();

	const queryClient = useQueryClient();

	const config = $derived(widget.config ?? {});
	const kind = $derived(config.resourceKind);
	const definition = $derived(kind ? RESOURCE_KINDS[kind] : undefined);
	const title = $derived(widget.title || (definition?.label() ?? m.filtered_resources()));
	const environments = $derived.by((): EnvironmentRef[] => {
		if (!definition) return [];
		const picked = config.environmentIds ?? [];
		return environmentStore.available.filter(
			(environment) =>
				shouldLoadEnvironment(environment) &&
				hasPermission(definition.permission, environment.id) &&
				(picked.length === 0 || picked.includes(environment.id))
		);
	});
	const request = $derived<SearchPaginationSortRequest>({
		search: config.search,
		filters: config.filters,
		pagination: { page: 1, limit: config.limit ?? DEFAULT_RESOURCE_LIMIT }
	});

	function toResult<T extends { id: string }>(
		page: ResourcePage<T>,
		describe: (item: EnvironmentRow<T>) => Pick<ResourceRow, 'name' | 'status' | 'variant' | 'href'>
	): ResourceResult {
		return {
			total: page.pagination.totalItems,
			rows: page.data.map((item) => ({
				...describe(item),
				key: `${item.environmentId}:${item.id}`,
				environmentId: item.environmentId
			})),
			failedEnvironments: page.failedEnvironments ?? []
		};
	}

	function inUseBadge(inUse: boolean): Pick<ResourceRow, 'status' | 'variant'> {
		return inUse ? { status: m.common_in_use(), variant: 'green' } : { status: m.common_unused(), variant: 'amber' };
	}

	async function loadResources(
		resourceKind: DashboardResourceKind,
		scope: string,
		targets: EnvironmentRef[],
		options: SearchPaginationSortRequest
	): Promise<ResourceResult> {
		switch (resourceKind) {
			case 'containers':
				return toResult(await queryClient.fetchQuery(containerListQuery(scope, targets, options)), (container) => ({
					name: getContainerDisplayName(container),
					status: getContainerStatusLabel(container.state),
					variant: getStateBadgeVariant(container.state),
					href: resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(container.environmentId)}`, {
						containerId: container.id
					})
				}));
			case 'projects':
				return toResult(await queryClient.fetchQuery(projectListQuery(scope, targets, options)), (project) => ({
					name: project.name,
					status: capitalizeFirstLetter(project.status),
					variant: getStatusVariant(project.status),
					href: resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(project.environmentId)}`, {
						projectId: project.id
					})
				}));
			case 'images':
				return toResult(await queryClient.fetchQuery(imageListQuery(scope, targets, options)), (image) => ({
					...inUseBadge(image.inUse),
					name: `${image.repo}:${image.tag}`,
					href: resolve(`/(app)/images/[imageId]?env=${encodeURIComponent(image.environmentId)}`, { imageId: image.id })
				}));
			case 'volumes':
				return toResult(await queryClient.fetchQuery(volumeListQuery(scope, targets, options)), (volume) => ({
					...inUseBadge(volume.inUse),
					name: volume.name,
					href: resolve(`/(app)/volumes/[volumeName]?env=${encodeURIComponent(volume.environmentId)}`, {
						volumeName: volume.id
					})
				}));
			case 'networks':
				return toResult(await queryClient.fetchQuery(networkListQuery(scope, targets, options)), (network) => ({
					...inUseBadge(network.inUse),
					name: network.name,
					href: resolve(`/(app)/networks/[networkId]?env=${encodeURIComponent(network.environmentId)}`, {
						networkId: network.id
					})
				}));
		}
	}

	const resourcesQuery = createQuery(() => {
		const resourceKind = kind;
		const scope = environments.map((environment) => environment.id).join(',');
		return {
			queryKey: [resourceKind, scope, 'dashboard', 'resource-query', request],
			queryFn:
				resourceKind && environments.length > 0 ? () => loadResources(resourceKind, scope, environments, request) : skipToken,
			placeholderData: keepPreviousData
		};
	});

	const errorMessage = $derived(resourcesQuery.error ? extractApiErrorMessage(resourcesQuery.error) : null);
	const loading = $derived(environments.length > 0 && resourcesQuery.isPending);
	const failedEnvironments = $derived(resourcesQuery.data?.failedEnvironments ?? []);
</script>

{#snippet failedNote()}
	{#if failedEnvironments.length > 0}
		<p class="text-xs text-muted-foreground">
			{m.all_environments_unavailable({
				environments: failedEnvironments.map(({ environment, message }) => `${environment.name} (${message})`).join(', ')
			})}
		</p>
	{/if}
{/snippet}

{#if config.display === 'list'}
	<DashboardPanel {title}>
		{#if !definition}
			<p class="py-2 text-sm text-muted-foreground">{m.common_not_configured()}</p>
		{:else if loading}
			<div class="space-y-3 py-2">
				<Skeleton class="h-4 w-3/4" />
				<Skeleton class="h-4 w-1/2" />
			</div>
		{:else if errorMessage}
			<div class="space-y-1 py-2">
				<p class="text-sm text-muted-foreground">{m.common_unavailable()}</p>
				<p class="text-xs whitespace-pre-line text-muted-foreground">{errorMessage}</p>
			</div>
		{:else if environments.length === 0}
			<p class="py-2 text-sm text-muted-foreground">{m.common_unavailable()}</p>
		{:else if (resourcesQuery.data?.rows.length ?? 0) === 0}
			<p class="py-2 text-sm text-muted-foreground">{m.common_no_results_found()}</p>
			{@render failedNote()}
		{:else}
			<ul class="divide-y divide-border/40">
				{#each resourcesQuery.data?.rows ?? [] as row (row.key)}
					<li>
						<a href={row.href} class="-mx-2 grid grid-cols-content-action items-center gap-3 rounded-md hover-glow px-2 py-2.5">
							<span class="min-w-0 space-y-1">
								<span class="block truncate text-sm font-medium">{row.name}</span>
								{#if environments.length > 1}
									<EnvironmentCell value={row.environmentId} />
								{/if}
							</span>
							<Badge variant={row.variant} size="sm">{row.status}</Badge>
						</a>
					</li>
				{/each}
			</ul>
			{@render failedNote()}
		{/if}
	</DashboardPanel>
{:else}
	<Card.Root interactive class="h-full min-w-0">
		<dl class="flex h-full w-full items-center justify-center p-5">
			<StatStrip.Item
				label={title}
				value={definition && !errorMessage && environments.length > 0 ? (resourcesQuery.data?.total ?? null) : null}
				icon={definition?.icon}
				href={definition?.listHref()}
				{loading}
			>
				{#if !definition}
					{m.common_not_configured()}
				{:else if errorMessage}
					<span class="line-clamp-2" title={errorMessage}>{errorMessage}</span>
				{:else if environments.length === 0}
					{m.common_unavailable()}
				{:else if environments.length > 1}
					{m.across_count_environments({ count: environments.length })}
				{/if}
			</StatStrip.Item>
		</dl>
	</Card.Root>
{/if}
