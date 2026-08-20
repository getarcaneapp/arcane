<script lang="ts">
	import { m } from '#lib/paraglide/messages';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { AlertTriangleIcon, DownloadIcon } from '#lib/icons';
	import { diagnosticsService } from '#lib/services/diagnostics-service';
	import type { GoroutineLeakReport } from '#lib/types/diagnostics';
	import { formatTime } from '#lib/utils/formatting';
	import { cn } from '#lib/utils';

	interface Props {
		leakedGoroutines: number;
		leakScannedAt?: string;
		onscanned?: (report: GoroutineLeakReport) => void;
	}

	let { leakedGoroutines, leakScannedAt, onscanned }: Props = $props();

	let scanning = $state(false);
	let downloading = $state(false);
	let error = $state<string | null>(null);
	let report = $state.raw<GoroutineLeakReport | null>(null);

	const scannedAt = $derived(report?.scannedAt ?? leakScannedAt);
	const count = $derived(report?.count ?? leakedGoroutines);
	const hasScan = $derived(Boolean(scannedAt) || count > 0);
	const profileText = $derived(report?.profile ?? '');

	async function scan() {
		scanning = true;
		error = null;
		try {
			const next = await diagnosticsService.scanGoroutineLeaks();
			report = next;
			onscanned?.(next);
		} catch (e) {
			error = e instanceof Error ? e.message : m.diagnostics_error_scan_leaks();
		} finally {
			scanning = false;
		}
	}

	async function download() {
		downloading = true;
		error = null;
		try {
			await diagnosticsService.downloadProfile('goroutineleak');
		} catch (e) {
			error = e instanceof Error ? e.message : m.diagnostics_error_download();
		} finally {
			downloading = false;
		}
	}
</script>

<div class="space-y-4">
	<p class="text-sm text-muted-foreground">
		{m.diagnostics_leaks_hint()}
		{m.diagnostics_leaks_limitations()}
	</p>

	<div class="flex flex-wrap items-center gap-3">
		<div
			class={cn(
				'rounded-lg border px-3 py-2 text-sm',
				hasScan && count > 0
					? 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
					: 'border-border/60 bg-card/40'
			)}
		>
			{#if !hasScan}
				{m.diagnostics_leaks_not_scanned()}
			{:else if count > 0}
				<span class="inline-flex items-center gap-1.5 font-medium">
					<AlertTriangleIcon class="size-4" />
					{m.diagnostics_leaks_count({ count })}
				</span>
			{:else}
				{m.diagnostics_leaks_none()}
			{/if}
			{#if scannedAt}
				<span class="ml-2 text-xs text-muted-foreground">
					{m.diagnostics_leaks_last_scan({ when: formatTime(scannedAt) })}
				</span>
			{/if}
		</div>

		<ArcaneButton
			action="scan"
			size="sm"
			customLabel={m.diagnostics_leaks_scan()}
			loading={scanning}
			disabled={scanning || downloading}
			onclick={scan}
		/>
		<ArcaneButton
			action="base"
			tone="outline"
			size="sm"
			icon={DownloadIcon}
			customLabel={m.diagnostics_profile_goroutineleak()}
			loading={downloading}
			disabled={scanning || downloading}
			onclick={download}
		/>
	</div>

	{#if error}
		<div class="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
			{error}
		</div>
	{/if}

	{#if profileText}
		<pre
			class="max-h-96 overflow-auto rounded-lg border border-border/60 bg-background p-3 font-mono text-[11px] leading-relaxed">{profileText}</pre>
	{:else if hasScan && count === 0}
		<p class="text-sm text-muted-foreground">{m.diagnostics_leaks_empty()}</p>
	{/if}
</div>
