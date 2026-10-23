<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import { untrack } from 'svelte';
	import type { Attachment } from 'svelte/attachments';

	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, AlertTriangleIcon, ExternalLinkIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { APIError } from '#lib/services/api-service.js';
	import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
	import { clientStream } from '#lib/stores/client-stream.svelte.js';
	import { MANAGER_ENVIRONMENT_ID, isActiveUpgradeJob, upgradeJobStore } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { AppVersionInformation } from '#lib/types/settings.js';
	import type { UpgradeJob, UpgradeJobResult, UpgradeJobResultStatus, UpgradeJobStage } from '#lib/types/system-upgrade.js';
	import { cn } from '#lib/utils.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { formatRelativeTime, nowInstantString } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import UpdateAllElapsed from './update-all-elapsed.svelte';
	import UpgradeJobRow from './upgrade-job-row.svelte';
	import VersionUpdateSummary from './version-update-summary.svelte';

	// open has no $bindable fallback: upstream binds can start out undefined, and
	// binding undefined to a $bindable with a fallback throws props_invalid_value.
	let {
		open = $bindable(undefined),
		versionInformation,
		canConfirm = true,
		debugDemo = false,
		onFinished
	}: {
		open?: boolean;
		versionInformation?: AppVersionInformation;
		canConfirm?: boolean;
		/** Dev-only: run a scripted fake set of environments instead of calling the API. */
		debugDemo?: boolean;
		onFinished?: () => void | Promise<void>;
	} = $props();

	let jobId = $state<string | null>(null);
	let starting = $state(false);
	let demoJob = $state<UpgradeJob | null>(null);
	let demoReconnecting = $state(false);
	let startAttempt = 0;

	const job = $derived.by(() => {
		if (debugDemo) return demoJob;
		const current = upgradeJobStore.job;
		return current && current.id === jobId ? current : null;
	});

	const phase = $derived.by(() => {
		if (debugDemo) {
			if (!demoJob) return 'confirm';
			return isActiveUpgradeJob(demoJob) ? 'running' : 'finished';
		}
		if (!jobId) return starting ? 'running' : 'confirm';
		return !job || isActiveUpgradeJob(job) ? 'running' : 'finished';
	});

	const reconnecting = $derived(
		debugDemo ? demoReconnecting : job?.status === 'pending_restart' && !clientStream.streamConnected
	);

	function resetState() {
		startAttempt++;
		jobId = null;
		starting = false;
		demoJob = null;
		demoReconnecting = false;
	}

	function followJob(next: UpgradeJob) {
		upgradeJobStore.follow(next);
		jobId = next.id;
		starting = false;
	}

	// Reopening the dialog picks up an update that is still running on the backend.
	async function restoreActiveJob() {
		const attempt = ++startAttempt;
		starting = true;
		await upgradeJobStore.load();
		if (attempt === startAttempt) starting = false;
	}

	$effect(() => {
		if (!open || debugDemo || jobId || starting) return;
		const current = upgradeJobStore.job;
		if (isActiveUpgradeJob(current)) untrack(() => followJob(current));
	});

	// Dialog.Content only exists while the dialog is open, so this runs once per
	// opening and its cleanup once per close.
	const observeUpdate: Attachment = () => {
		if (debugDemo) return () => resetState();
		const stopWatching = upgradeJobStore.watch();
		void restoreActiveJob();
		return () => {
			stopWatching();
			resetState();
		};
	};

	async function handleConfirm(environmentIds?: string[]) {
		const attempt = ++startAttempt;
		starting = true;

		if (debugDemo) {
			void runDebugDemo();
			return;
		}

		const startResult = await tryCatch(systemUpgradeService.startUpgradeJob(environmentIds));
		let next = startResult.data;
		// The backend refuses a second job while one is active (409). Adopt that job
		// and follow it instead of failing; never retry the POST.
		if (startResult.error instanceof APIError && startResult.error.status === 409) {
			const active = await upgradeJobStore.load();
			if (isActiveUpgradeJob(active)) next = active;
		}
		// The dialog closed (or was re-confirmed) while the request was in flight.
		if (attempt !== startAttempt) return;

		if (!next) {
			await handleApiResultWithCallbacks({ result: startResult, message: m.environments_update_all_trigger_failed() });
			if (attempt === startAttempt) starting = false;
			return;
		}

		followJob(next);
	}

	async function handleClose() {
		const attempt = ++startAttempt;
		// Refresh through SvelteKit instead of reloading the document: a hard reload
		// lands while agents are still reconnecting after every environment restarts, which
		// renders every environment as offline for a moment.
		if (job && !debugDemo) await tryCatch(refreshAll());
		if (attempt !== startAttempt) return;
		resetState();
		open = false;
		await onFinished?.();
	}

	// Version/release presentation for the confirm step, rendered when the caller
	// has the manager's version information at hand (sidebar / mobile nav).
	const releaseNotes = $derived(versionInformation?.releaseNotes?.trim() ?? '');
	const releaseUrl = $derived(versionInformation?.releaseUrl ?? '');
	const releasedAgo = $derived.by(() => {
		const at = versionInformation?.releasedAt;
		return at ? formatRelativeTime(at) : '';
	});

	// "Done" is every environment that has reached a terminal state — i.e. anything
	// that isn't still queued (pending) or actively being worked on (updating).
	const results = $derived(job?.results ?? []);
	const managerResult = $derived(results.find((r) => r.environmentId === MANAGER_ENVIRONMENT_ID));
	const totalCount = $derived(results.length);
	const doneCount = $derived(results.filter((r) => r.status !== 'pending' && r.status !== 'updating').length);
	const outcomeCounts = $derived({
		updated: results.filter((r) => r.status === 'updated').length,
		current: results.filter((r) => r.status === 'up_to_date').length,
		skipped: results.filter((r) => r.status === 'skipped_offline').length,
		failed: results.filter((r) => r.status === 'failed').length,
		unconfirmed: results.filter((r) => r.status === 'triggered').length
	});
	const versionChange = $derived.by(() => {
		const from = job?.managerVersionAtStart?.trim() ?? '';
		const to = job?.managerTargetVersion?.trim() ?? '';
		return from && to && from !== to ? `${from} → ${to}` : '';
	});
	const hasIssues = $derived(outcomeCounts.failed > 0 || outcomeCounts.unconfirmed > 0);
	const failed = $derived(job?.status === 'failed' || hasIssues);

	const title = $derived.by(() => {
		if (phase === 'confirm') return m.environments_update_all_title();
		if (phase === 'finished') {
			if (job?.status === 'failed') return m.environments_update_all_failed();
			return hasIssues ? m.environments_update_all_finished_with_issues() : m.environments_update_all_completed();
		}
		return m.environments_update_all_in_progress();
	});

	const orderedResults = $derived([
		...results.filter((r) => r.environmentId !== MANAGER_ENVIRONMENT_ID),
		...(managerResult ? [managerResult] : [])
	]);
	const failedIds = $derived(results.filter((r) => r.status === 'failed').map((r) => r.environmentId));
	const updatingCount = $derived(results.filter((r) => r.status === 'updating').length);
	const progressSegments = $derived([
		{ count: outcomeCounts.updated + outcomeCounts.current, class: 'bg-success', label: m.environments_update_all_legend_done },
		{ count: updatingCount, class: 'bg-primary animate-pulse', label: m.environments_update_all_legend_updating },
		{
			count: outcomeCounts.skipped + outcomeCounts.unconfirmed,
			class: 'bg-warning',
			label: m.environments_update_all_legend_skipped
		},
		{ count: outcomeCounts.failed, class: 'bg-destructive', label: m.environments_update_all_legend_failed },
		{ count: totalCount - doneCount - updatingCount, class: 'bg-muted', label: m.environments_update_all_legend_queued }
	]);

	// Dev-only preview: step fake environments through every row state so this dialog can be
	// designed and reviewed without triggering a real update of all environments. Only ever enabled
	// from a dev-guarded callsite.
	async function runDebugDemo() {
		const attempt = startAttempt;
		const live = () => attempt === startAttempt && isActiveUpgradeJob(demoJob);
		// Sleeps, then reports whether the demo is still the one being watched.
		const pause = async (ms: number) => {
			await new Promise((resolve) => setTimeout(resolve, ms));
			return live();
		};
		const enterStage = (row: UpgradeJobResult, stage: UpgradeJobStage, percent: number) => {
			row.stage = stage;
			row.stageStartedAt = nowInstantString();
			row.percent = percent;
			row.logs = [...(row.logs ?? []), `time=${nowInstantString()} level=INFO msg="${stage}"`];
		};
		// Enough environments (10) to exercise the scrolling list (#3655).
		const demo: Array<{ name: string; outcome: UpgradeJobResultStatus; to: string; error?: string }> = [
			{ name: 'Local Docker', outcome: 'updated', to: 'v0.9.2' },
			{ name: 'palladium', outcome: 'updated', to: 'v0.9.2' },
			{ name: 'naswidc1.ofkm.us', outcome: 'up_to_date', to: 'v0.9.1' },
			{ name: 'ofkm-cloud', outcome: 'skipped_offline', to: '' },
			{ name: 'parquetide', outcome: 'failed', to: '', error: 'dial tcp 10.0.0.9:3552: connect: connection refused' },
			{ name: 'edge-nuc-01', outcome: 'updated', to: 'v0.9.2' },
			{ name: 'edge-nuc-02', outcome: 'updated', to: 'v0.9.2' },
			{ name: 'hetzner-fsn1', outcome: 'triggered', to: 'v0.9.2' },
			{ name: 'homelab-pi5', outcome: 'up_to_date', to: 'v0.9.1' },
			{ name: 'staging.ofkm.us', outcome: 'updated', to: 'v0.9.2' }
		];

		demoJob = {
			id: 'demo',
			status: 'running',
			createdAt: nowInstantString(),
			userId: 'demo',
			username: 'demo',
			managerVersionAtStart: 'v0.9.1',
			managerDigestAtStart: '',
			managerTargetVersion: 'v0.9.2',
			results: demo.map((entry, index) => ({
				environmentId: index === 0 ? MANAGER_ENVIRONMENT_ID : `demo-${index}`,
				environmentName: entry.name,
				status: 'pending',
				fromVersion: 'v0.9.1',
				percent: 0
			}))
		};
		starting = false;

		// Remotes four at a time, manager last — the real processing order.
		const runRow = async (index: number, entry: (typeof demo)[number]) => {
			const row = demoJob?.results?.[index];
			if (!live() || !row) return false;
			row.status = 'updating';
			enterStage(row, 'checking', 5);
			if (!(await pause(700))) return false;
			enterStage(row, 'pulling', 30);
			if (!(await pause(700))) return false;
			enterStage(row, 'upgrading', 70);
			if (!(await pause(700))) return false;
			enterStage(row, 'reconnecting', 90);
			// One slow reconnect, to exercise the elapsed-time display.
			if (index === 1 && !(await pause(5000))) return false;
			// The manager restart is the one moment the reconnecting banner shows.
			if (index === 0 && demoJob) {
				demoJob.status = 'pending_restart';
				demoReconnecting = true;
				if (!(await pause(3000))) return false;
				demoReconnecting = false;
			}
			enterStage(row, 'verifying', 95);
			if (!(await pause(700))) return false;
			row.status = entry.outcome;
			row.stage = undefined;
			row.stageStartedAt = undefined;
			row.percent = 100;
			row.toVersion = entry.to;
			if (entry.error) row.error = entry.error;
			return true;
		};
		const remotes = [...demo.entries()].slice(1);
		for (let start = 0; start < remotes.length; start += 4) {
			const batch = remotes.slice(start, start + 4).map(([index, entry]) => runRow(index, entry));
			if ((await Promise.all(batch)).includes(false)) return;
		}
		if (!demo[0] || !(await runRow(0, demo[0]))) return;

		if (!live() || !demoJob) return;
		demoJob.status = 'completed';
		demoJob.completedAt = nowInstantString();
	}
</script>

<Dialog.Root {open} onOpenChange={(next) => (open = next)}>
	<Dialog.Content
		{@attach observeUpdate}
		sectioned
		class="flex max-h-(--max-height-dscreen-90) flex-col overflow-hidden sm:max-w-130"
	>
		{#if phase === 'confirm'}
			<div class="px-6 pt-6 pb-4">
				<Dialog.Header>
					<div class="space-y-3">
						<Dialog.Title>{title}</Dialog.Title>
						<Dialog.Description>{m.environments_update_all_message()}</Dialog.Description>
						{#if versionInformation}
							<VersionUpdateSummary {versionInformation} {releasedAgo} />
						{/if}
					</div>
				</Dialog.Header>
			</div>

			{#if versionInformation}
				<div class="flex min-h-0 flex-1 flex-col border-t border-border/60">
					<div class="flex items-center justify-between px-6 pt-4 pb-2">
						<h3 class="text-sm font-semibold text-foreground">{m.update_center_whats_new()}</h3>
						{#if releaseUrl}
							<a
								href={releaseUrl}
								target="_blank"
								rel="noopener noreferrer"
								class="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
							>
								{m.update_center_view_full_release()}
								<ExternalLinkIcon class="size-3" />
							</a>
						{/if}
					</div>
					<div class="min-h-0 flex-1 overflow-y-auto px-6 pb-4">
						{#if releaseNotes}
							<ReleaseNotes markdown={releaseNotes} />
						{:else}
							<p class="text-sm text-muted-foreground italic">{m.update_center_release_notes_unavailable()}</p>
						{/if}
					</div>
				</div>
			{/if}
		{:else}
			<div class="px-6 pt-6 pb-4">
				<Dialog.Header>
					<div class="flex items-center justify-between gap-3">
						<div class="flex min-w-0 items-center gap-2">
							{#if phase === 'finished'}
								{#if failed}
									<AlertTriangleIcon class="size-4 shrink-0 text-destructive" />
								{:else}
									<SuccessIcon class="size-4 shrink-0 text-success" />
								{/if}
							{/if}
							<Dialog.Title><span class="block min-w-0 truncate">{title}</span></Dialog.Title>
						</div>
						{#if totalCount > 0}
							<span class="shrink-0 pr-6 text-xs text-muted-foreground tabular-nums">
								{m.environments_update_all_progress({ done: doneCount, total: totalCount })}
							</span>
						{/if}
					</div>
					{#if versionChange || (phase === 'running' && job)}
						<p class="flex flex-wrap gap-x-2 text-xs text-muted-foreground tabular-nums">
							{#if versionChange}<span>{versionChange}</span>{/if}
							{#if phase === 'running' && job}<UpdateAllElapsed since={job.createdAt} />{/if}
						</p>
					{/if}
				</Dialog.Header>

				{#if totalCount > 0}
					<div class="mt-4 flex h-1.5 overflow-hidden rounded-full bg-muted">
						{#each progressSegments as segment, index (index)}
							{#if segment.count > 0}
								<div
									class={cn('h-full w-(--segment-width) transition-all', segment.class)}
									style:--segment-width="{(segment.count / totalCount) * 100}%"
								></div>
							{/if}
						{/each}
					</div>
					<div class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground tabular-nums">
						{#each progressSegments as segment, index (index)}
							{#if segment.count > 0}
								<span class="inline-flex items-center gap-1.5">
									<span class={cn('size-1.5 rounded-full', segment.class)}></span>
									{segment.label({ count: segment.count })}
								</span>
							{/if}
						{/each}
					</div>
				{/if}

				{#if phase === 'finished' && job?.error}
					<p class="mt-3 text-sm text-destructive">{job.error}</p>
				{/if}

				{#if reconnecting}
					<div
						class="mt-4 flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/5 px-3 py-2.5 text-sm text-warning"
					>
						<Spinner class="size-4" />
						<div>
							<span>{m.environments_update_all_manager_restarting()}</span>
							<UpdateAllElapsed class="block text-xs" since={managerResult?.stageStartedAt ?? job?.createdAt} />
						</div>
					</div>
				{/if}
			</div>

			{#if totalCount > 0}
				<div class="border-t border-border/60">
					<!-- Native scroller: ScrollArea's percentage-height viewport resolves to auto
					     under a max-height-only parent, so it never clips (#3655). -->
					<div class="max-h-96 overflow-y-auto">
						<ul class="divide-y divide-border/50">
							{#each orderedResults as result (result.environmentId)}
								<UpgradeJobRow {result} />
							{/each}
						</ul>
					</div>
				</div>
			{:else}
				<div class="flex items-center gap-2 border-t border-border/60 px-6 py-4 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					<span>{job ? m.environments_update_all_in_progress() : m.environments_update_all_loading_status()}</span>
				</div>
			{/if}
		{/if}

		<Dialog.Footer>
			{#if phase === 'confirm'}
				<Button variant="outline" onclick={() => (open = false)}>{m.common_cancel()}</Button>
				{#if canConfirm}
					<Button onclick={() => handleConfirm()}>{m.update_all()}</Button>
				{/if}
			{:else}
				{#if phase === 'finished' && canConfirm && !debugDemo && failedIds.length > 0}
					<Button variant="outline" disabled={starting} onclick={() => handleConfirm(failedIds)}>
						{m.environments_update_all_retry_failed()}
					</Button>
				{/if}
				<Button variant="outline" onclick={handleClose}>{m.common_close()}</Button>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
