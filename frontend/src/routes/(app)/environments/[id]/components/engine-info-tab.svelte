<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { EmptyState } from '#lib/components/states/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, AlertTriangleIcon, CheckIcon, CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { systemService } from '#lib/services/system-service.js';
	import type { PodmanStatus } from '#lib/types/podman.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';

	import { dockerEngineView, podmanEngineView, type EngineRow, type EngineView } from './engine-info';

	let { environmentId, podmanStatus }: { environmentId: string; podmanStatus?: PodmanStatus } = $props();

	const dockerInfoQuery = createQuery(() => ({
		queryKey: queryKeys.system.dockerInfo(environmentId),
		queryFn: () => systemService.getDockerInfoForEnvironment(environmentId),
		enabled: !podmanStatus,
		retry: false
	}));
	const podmanInfoQuery = createQuery(() => ({
		queryKey: queryKeys.podman.info(environmentId),
		queryFn: () => podmanService.getInfo(environmentId),
		enabled: !!podmanStatus,
		retry: false
	}));
	const activeQuery = $derived(podmanStatus ? podmanInfoQuery : dockerInfoQuery);
	const view = $derived.by((): EngineView | null => {
		if (podmanStatus) return podmanInfoQuery.data ? podmanEngineView(podmanInfoQuery.data, podmanStatus) : null;
		return dockerInfoQuery.data ? dockerEngineView(dockerInfoQuery.data) : null;
	});
</script>

{#snippet row(entry: EngineRow)}
	{#if entry.kind === 'info'}
		<div class="grid grid-cols-label-value items-baseline gap-x-4 py-2">
			<span class="text-sm text-muted-foreground">{entry.label}</span>
			<span class="text-right text-sm wrap-anywhere {entry.mono ? 'font-mono' : ''}">
				{entry.value === undefined || entry.value === null || entry.value === '' ? '-' : entry.value}
			</span>
		</div>
	{:else if entry.kind === 'tags'}
		<div class={entry.label ? 'grid grid-cols-label-value items-start gap-x-4 py-2.5' : 'py-3'}>
			{#if entry.label}
				<span class="text-sm text-muted-foreground">{entry.label}</span>
			{/if}
			<div class="flex flex-wrap gap-1.5 {entry.label ? 'justify-end' : ''}">
				{#each withOccurrenceKeys(entry.items) as { value: item, key } (key)}
					<Badge variant="outline" mono={entry.mono}>{item}</Badge>
				{:else}
					<span class="text-sm text-muted-foreground">-</span>
				{/each}
			</div>
		</div>
	{:else if entry.kind === 'block'}
		<div class="space-y-1.5 py-2.5">
			<div class="text-sm text-muted-foreground">{entry.label}</div>
			<div class="max-h-48 overflow-y-auto rounded-md bg-muted/30 px-3 py-2 font-mono text-xs leading-relaxed break-all">
				{entry.value}
			</div>
		</div>
	{:else}
		<div class="grid grid-cols-label-value items-center gap-x-4 py-2">
			<span class="text-sm text-muted-foreground">{m.docker_info_git_commit_label()}</span>
			<div class="flex items-center justify-end gap-1.5">
				<code class="rounded-xl bg-muted px-1.5 py-0.5 font-mono text-sm">{entry.value?.slice(0, 8) ?? '-'}</code>
				{#if entry.value}
					<CopyButton text={entry.value} size="icon" class="size-6" title={m.docker_copy_commit_hash()} />
				{/if}
			</div>
		</div>
	{/if}
{/snippet}

{#if activeQuery.error}
	<EmptyState icon={AlertIcon} title={m.common_failed()} description={extractApiErrorMessage(activeQuery.error)} />
{:else if view}
	<div class="space-y-4">
		{#if view.warnings.length > 0}
			<Alert.Root variant="warning-subtle" icon={AlertTriangleIcon} heading={m.docker_info_warnings_section()}>
				<ul class="space-y-1">
					{#each withOccurrenceKeys(view.warnings) as { value: warning, key } (key)}
						<li class="text-sm wrap-anywhere">{warning}</li>
					{/each}
				</ul>
			</Alert.Root>
		{/if}

		<div class="flex flex-wrap items-end gap-x-8 gap-y-3 border-b border-border/50 pb-4">
			{#each view.stats as stat (stat.label)}
				<div class="flex flex-col gap-0.5">
					<div class="flex items-center gap-1.5 text-xs whitespace-nowrap text-muted-foreground">
						{#if stat.dot}
							<span class="size-2 shrink-0 rounded-full {stat.dot}"></span>
						{/if}
						{stat.label}
					</div>
					<span class="text-xl font-semibold tabular-nums">{stat.value ?? '-'}</span>
				</div>
			{/each}
		</div>

		<div class="columns-1 gap-4 lg:columns-2 2xl:columns-3 [&>*]:mb-4 [&>*]:break-inside-avoid">
			{#each view.sections as section (section.title)}
				<section class="divide-y divide-border/40">
					<h3 class="pb-2 text-sm font-semibold">{section.title}</h3>
					{#if section.caps}
						<div class="grid grid-cols-1 gap-x-6 gap-y-2 py-3 sm:grid-cols-2">
							{#each section.caps as cap (cap.label)}
								<div class="flex items-center justify-between gap-3">
									<span class="text-sm wrap-anywhere text-muted-foreground">{cap.label}</span>
									{#if cap.value}
										<CheckIcon class="size-4 shrink-0 text-success" />
										<span class="sr-only">{m.common_yes()}</span>
									{:else}
										<CloseIcon class="size-4 shrink-0 text-muted-foreground/50" />
										<span class="sr-only">{m.common_no()}</span>
									{/if}
								</div>
							{/each}
						</div>
					{/if}
					{#if section.rows.length > 0}
						<div class="divide-y divide-border/40">
							{#each section.rows as entry, index (index)}
								{@render row(entry)}
							{/each}
						</div>
					{/if}
				</section>
			{/each}
		</div>
	</div>
{:else}
	<div class="flex min-h-56 flex-col items-center justify-center gap-3">
		<Spinner class="size-5" />
		<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
	</div>
{/if}
