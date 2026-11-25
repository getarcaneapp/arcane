<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { imageBuildListQuery } from '#lib/query/images.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ImageBuildRecord, ImageBuildStatus } from '#lib/types/image.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { readableEnvironments } from '#lib/utils/environments.js';

	import { buildHistoryStatusLabel, formatBuildTimestamp, getBuildTitle } from '../image-build-history';

	let {
		onApplyBuild,
		onOpenHistory
	}: {
		onApplyBuild: (build: ImageBuildRecord, environmentId: string) => void;
		onOpenHistory: () => void;
	} = $props();

	const requestOptions = {
		pagination: { page: 1, limit: 10 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest;
	const statusDotClass = {
		running: 'bg-info',
		success: 'bg-success',
		failed: 'bg-destructive'
	} satisfies Record<ImageBuildStatus, string>;

	let open = $state(false);
	const historyQuery = createQuery(() => ({
		...imageBuildListQuery(environmentStore.shownKey, readableEnvironments('images:list'), requestOptions),
		enabled: open
	}));
	const builds = $derived(historyQuery.data?.data ?? []);
</script>

<DropdownMenu.Root bind:open>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{...props}
				action="base"
				tone="outline"
				size="sm"
				icon={ClockIcon}
				showLabel={false}
				aria-label={m.build_history()}
				title={m.build_history()}
				class="shrink-0"
			>
				<span class="hidden sm:inline">{m.build_history()}</span>
			</ArcaneButton>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end" class="w-80">
		<DropdownMenu.Label>{m.build_history()}</DropdownMenu.Label>
		{#if historyQuery.isPending}
			<p class="px-2 py-1.5 text-sm text-muted-foreground" role="status">{m.common_loading()}</p>
		{:else if historyQuery.error}
			<p class="px-2 py-1.5 text-sm text-muted-foreground" role="status">{extractApiErrorMessage(historyQuery.error)}</p>
		{:else if builds.length === 0}
			<p class="px-2 py-1.5 text-sm text-muted-foreground" role="status">{m.image_build_history_empty_title()}</p>
		{:else}
			{#each builds as build (`${build.environmentId}:${build.id}`)}
				<DropdownMenu.Item
					disabled={!hasPermission('images:build', build.environmentId)}
					onclick={() => onApplyBuild(build, build.environmentId)}
				>
					<span class="inline-flex max-w-1/2 shrink-0 items-center overflow-hidden rounded-full border px-2 py-0.5">
						<EnvironmentCell value={build.environmentId} />
					</span>
					<span class="min-w-0 flex-1">
						<span class="block font-medium wrap-anywhere">{build.tags?.join(', ') || getBuildTitle(build)}</span>
						<span class="block text-xs wrap-anywhere text-muted-foreground">{formatBuildTimestamp(build.createdAt)}</span>
					</span>
					<span
						role="img"
						aria-label={buildHistoryStatusLabel(build.status)}
						class={['size-2 shrink-0 rounded-full', statusDotClass[build.status]]}
					></span>
				</DropdownMenu.Item>
			{/each}
		{/if}
		<DropdownMenu.Separator />
		<DropdownMenu.Item onclick={onOpenHistory}>{m.common_view_all()}</DropdownMenu.Item>
	</DropdownMenu.Content>
</DropdownMenu.Root>
