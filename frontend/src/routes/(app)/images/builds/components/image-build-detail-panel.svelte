<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { CloseIcon, RedeployIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ImageBuildRecord } from '#lib/types/image.js';

	import {
		buildHistoryStatusLabel,
		formatBuildDuration,
		formatBuildTimestamp,
		getBuildStatusBadgeVariant,
		getBuildTitle,
		parseBuildOutput
	} from '../image-build-history';

	let {
		build,
		environmentId,
		loading,
		onClose,
		onRebuild
	}: {
		build: ImageBuildRecord;
		environmentId: string;
		loading: boolean;
		onClose: () => void;
		onRebuild: () => void;
	} = $props();

	const outputEntries = $derived(parseBuildOutput(build.output ?? ''));
	const created = $derived(
		build.durationMs
			? `${formatBuildTimestamp(build.createdAt)} · ${formatBuildDuration(build.durationMs)}`
			: formatBuildTimestamp(build.createdAt)
	);
	const details = $derived([
		{ label: m.build_context(), value: build.contextDir || '-', mono: true },
		{ label: m.build_provider(), value: build.provider || '-', mono: false },
		{ label: m.common_created(), value: created, mono: false },
		{ label: m.platforms_label(), value: build.platforms?.join(', ') || '-', mono: true }
	]);
</script>

<aside class="flex min-w-0 flex-col gap-3">
	<div class="flex items-start gap-2">
		<div class="flex min-w-0 flex-1 flex-wrap items-center gap-2">
			<span class="font-mono text-sm font-medium break-all">{getBuildTitle(build)}</span>
			<Badge variant={getBuildStatusBadgeVariant(build.status)} size="sm">{buildHistoryStatusLabel(build.status)}</Badge>
		</div>
		<ArcaneButton
			action="base"
			tone="ghost"
			size="icon"
			icon={CloseIcon}
			showLabel={false}
			customLabel={m.common_close()}
			onclick={onClose}
		/>
	</div>

	{#if build.errorMessage}
		<div class="rounded-xl border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
			{build.errorMessage}
		</div>
	{/if}

	<dl class="divide-y divide-border/50 text-xs">
		{#each details as detail (detail.label)}
			<div class="grid grid-cols-aside-28 gap-2 py-2">
				<dt class="text-muted-foreground">{detail.label}</dt>
				<dd class={detail.mono ? 'font-mono break-all' : 'break-words'}>{detail.value}</dd>
			</div>
		{/each}
	</dl>

	<div class="flex min-h-0 flex-col overflow-hidden rounded-xl bg-table-head">
		<div class="flex h-10 shrink-0 items-center justify-between gap-2 px-3">
			<h3 class="text-xs font-medium">{m.build_output()}</h3>
			{#if build.outputTruncated}
				<span class="text-xs text-warning">{m.build_output_truncated()}</span>
			{/if}
		</div>
		<div class="max-h-96 min-h-40 overflow-auto px-3 pb-3 font-mono text-xs leading-relaxed">
			{#if loading}
				<div class="flex h-full min-h-40 items-center justify-center">
					<Spinner tone="muted" class="size-5" />
				</div>
			{:else if outputEntries.length > 0}
				{#each outputEntries as entry, entryIndex (entry.text + entryIndex)}
					<div class={entry.isError ? 'break-words whitespace-pre-wrap text-destructive' : 'break-words whitespace-pre-wrap'}>
						{entry.text}
					</div>
				{/each}
			{:else}
				<div class="font-sans text-sm text-muted-foreground">{m.build_output_placeholder()}</div>
			{/if}
		</div>
	</div>

	<IfPermitted perm="images:build" envId={environmentId}>
		<div>
			<ArcaneButton
				action="base"
				tone="outline-primary"
				icon={RedeployIcon}
				customLabel={m.build_rebuild()}
				onclick={onRebuild}
			/>
		</div>
	</IfPermitted>
</aside>
