<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import type { IgnoredVulnerability } from '$lib/types/vulnerability.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { ShieldAlertIcon, CodeIcon, ImagesIcon, EyeOnIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button';

	let {
		ignoredVulnerabilities,
		requestOptions = $bindable(),
		isLoading,
		onRefresh,
		onUnignore
	}: {
		ignoredVulnerabilities: Paginated<IgnoredVulnerability>;
		requestOptions: SearchPaginationSortRequest;
		isLoading: boolean;
		onRefresh: (options: SearchPaginationSortRequest) => void;
		onUnignore: (ignoreId: string) => void;
	} = $props();

	const DEFAULT_PAGE_SIZE = 20;

	function formatDate(dateString: string): string {
		const date = new Date(dateString);
		return date.toLocaleDateString();
	}

	function handlePageChange(page: number) {
		const newOptions: SearchPaginationSortRequest = {
			...requestOptions,
			pagination: {
				page,
				limit: requestOptions.pagination?.limit ?? DEFAULT_PAGE_SIZE
			}
		};
		onRefresh(newOptions);
	}
</script>

<div class="divide-border divide-y">
	{#if ignoredVulnerabilities.data.length === 0}
		<div class="text-muted-foreground flex h-32 items-center justify-center">
			{#if isLoading}
				<div class="flex items-center gap-2">
					<div class="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
					<span>{m.common_loading()}</span>
				</div>
			{:else}
				{m.vuln_ignored_empty()}
			{/if}
		</div>
	{:else}
		{#each ignoredVulnerabilities.data as item (item.id)}
			<div class="hover:bg-muted/50 flex items-center justify-between p-4">
				<div class="flex-1 space-y-1">
					<div class="flex items-center gap-2">
						<ShieldAlertIcon class="text-muted-foreground h-4 w-4" />
						<a
							href="https://nvd.nist.gov/vuln/detail/{item.vulnerabilityId}"
							target="_blank"
							rel="noopener noreferrer"
							class="font-mono text-sm font-medium text-blue-600 hover:underline dark:text-blue-400"
						>
							{item.vulnerabilityId}
						</a>
					</div>
					<div class="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
						<span class="flex items-center gap-1">
							<CodeIcon class="h-3 w-3" />
							<span class="font-mono">{item.pkgName}@{item.installedVersion}</span>
						</span>
						<span class="flex items-center gap-1">
							<ImagesIcon class="h-3 w-3" />
							<span class="max-w-[200px] truncate" title={item.imageId}>
								{item.imageId.substring(0, 12)}...
							</span>
						</span>
						<span>• {formatDate(item.createdAt)}</span>
					</div>
					{#if item.reason}
						<div class="text-muted-foreground text-xs italic">
							{item.reason}
						</div>
					{/if}
				</div>
				<ArcaneButton
					action="base"
					size="sm"
					icon={EyeOnIcon}
					customLabel={m.vuln_unignore()}
					onclick={() => onUnignore(item.id)}
					disabled={isLoading}
				/>
			</div>
		{/each}

		{#if ignoredVulnerabilities.pagination.totalPages > 1}
			<div class="flex items-center justify-between border-t px-4 py-3">
				<div class="text-muted-foreground text-xs">
					{m.pagination_showing({
						from: (ignoredVulnerabilities.pagination.currentPage - 1) * ignoredVulnerabilities.pagination.itemsPerPage + 1,
						to: Math.min(
							ignoredVulnerabilities.pagination.currentPage * ignoredVulnerabilities.pagination.itemsPerPage,
							ignoredVulnerabilities.pagination.totalItems
						),
						total: ignoredVulnerabilities.pagination.totalItems
					})}
				</div>
				<div class="flex items-center gap-2">
					<ArcaneButton
						action="base"
						tone="outline"
						size="sm"
						onclick={() => handlePageChange(ignoredVulnerabilities.pagination.currentPage - 1)}
						disabled={ignoredVulnerabilities.pagination.currentPage <= 1 || isLoading}
					>
						{m.common_previous()}
					</ArcaneButton>
					<span class="text-muted-foreground text-xs">
						{ignoredVulnerabilities.pagination.currentPage} / {ignoredVulnerabilities.pagination.totalPages}
					</span>
					<ArcaneButton
						action="base"
						tone="outline"
						size="sm"
						onclick={() => handlePageChange(ignoredVulnerabilities.pagination.currentPage + 1)}
						disabled={ignoredVulnerabilities.pagination.currentPage >= ignoredVulnerabilities.pagination.totalPages || isLoading}
					>
						{m.common_next()}
					</ArcaneButton>
				</div>
			</div>
		{/if}
	{/if}
</div>
