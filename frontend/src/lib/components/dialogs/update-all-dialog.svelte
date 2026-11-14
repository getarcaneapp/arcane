<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import type { Attachment } from 'svelte/attachments';
	import { slide } from 'svelte/transition';

	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, AlertIcon, ArrowRightIcon, ExternalLinkIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { APIError } from '#lib/services/api-service.js';
	import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
	import { clientStream } from '#lib/stores/client-stream.svelte.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { MANAGER_ENVIRONMENT_ID, isActiveUpgradeJob, upgradeJobStore } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { UpgradeJob, UpgradeJobResult, UpgradeJobResultStatus, UpgradeJobStage } from '#lib/types/system.js';
	import type { VersionInfo } from '#lib/types/version.js';
	import { cn } from '#lib/utils.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { isEnvironmentOnline } from '#lib/utils/docker.js';
	import { formatRelativeTime, nowInstantString } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import UpdateAllElapsed from './update-all-elapsed.svelte';
	import UpgradeJobDetail from './upgrade-job-detail.svelte';
	import UpgradeQueueRow from './upgrade-queue-row.svelte';
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
		versionInformation?: VersionInfo;
		canConfirm?: boolean;
		/** Dev-only: run a scripted fake set of environments instead of calling the API. */
		debugDemo?: boolean;
		onFinished?: () => void | Promise<void>;
	} = $props();

	let jobId = $state<string | null>(null);
	let starting = $state(false);
	let demoJob = $state<UpgradeJob | null>(null);
	let demoReconnecting = $state(false);
	let pinnedId = $state<string | null>(null);
	let foldToggles = $state<Record<string, boolean>>({});
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
		pinnedId = null;
		foldToggles = {};
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
		const current = await upgradeJobStore.load();
		if (attempt !== startAttempt) return;
		if (isActiveUpgradeJob(current)) {
			followJob(current);
			return;
		}
		starting = false;
	}

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
		if (job && !debugDemo) await tryCatch(refreshAll());
		if (attempt !== startAttempt) return;
		resetState();
		open = false;
		await onFinished?.();
	}

	const releaseNotes = $derived(versionInformation?.releaseNotes?.trim() ?? '');
	const releaseUrl = $derived(versionInformation?.releaseUrl ?? '');
	const releasedAgo = $derived.by(() => {
		const at = versionInformation?.releasedAt;
		return at ? formatRelativeTime(at) : '';
	});

	const planOfflineCount = $derived(environmentStore.available.filter((env) => !isEnvironmentOnline(env)).length);
	const planOpen = $derived(foldToggles['plan'] ?? false);
	const planEnvironments = $derived([
		...environmentStore.available.filter((env) => env.id !== MANAGER_ENVIRONMENT_ID),
		...environmentStore.available.filter((env) => env.id === MANAGER_ENVIRONMENT_ID)
	]);

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
		{ count: outcomeCounts.updated + outcomeCounts.current, class: 'bg-success' },
		{ count: updatingCount, class: 'bg-primary animate-pulse' },
		{ count: outcomeCounts.skipped + outcomeCounts.unconfirmed, class: 'bg-warning' },
		{ count: outcomeCounts.failed, class: 'bg-destructive' },
		{ count: totalCount - doneCount - updatingCount, class: 'bg-muted' }
	]);
	const doneFoldable = $derived(outcomeCounts.updated + outcomeCounts.current < totalCount);
	const queueGroups = $derived([
		{
			key: 'failed',
			label: m.environments_update_all_legend_failed,
			foldable: false,
			open: true,
			results: orderedResults.filter((r) => r.status === 'failed')
		},
		{
			key: 'updating',
			label: m.environments_update_all_legend_updating,
			foldable: false,
			open: true,
			results: orderedResults.filter((r) => r.status === 'updating')
		},
		{
			key: 'queued',
			label: m.environments_update_all_legend_queued,
			foldable: true,
			open: foldToggles['queued'] ?? false,
			results: orderedResults.filter((r) => r.status === 'pending')
		},
		{
			key: 'skipped',
			label: m.environments_update_all_legend_skipped,
			foldable: false,
			open: true,
			results: orderedResults.filter((r) => r.status === 'skipped_offline' || r.status === 'triggered')
		},
		{
			key: 'done',
			label: m.environments_update_all_legend_done,
			foldable: doneFoldable,
			open: foldToggles['done'] ?? (!doneFoldable || outcomeCounts.updated + outcomeCounts.current <= 5),
			results: orderedResults.filter((r) => r.status === 'updated' || r.status === 'up_to_date')
		}
	]);
	const selectedResult = $derived(
		results.find((r) => r.environmentId === pinnedId) ??
			orderedResults.find((r) => r.status === (phase === 'finished' ? 'failed' : 'updating')) ??
			orderedResults[0]
	);

	async function runDebugDemo() {
		const attempt = startAttempt;
		const live = () => attempt === startAttempt && isActiveUpgradeJob(demoJob);
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

		// Remotes in batches of the parallel upgrade setting, manager last — the real processing order.
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
		const parallelAgents = Number(settingsStore.current?.upgradeMaxParallelAgents) || 4;
		const remotes = [...demo.entries()].slice(1);
		for (let start = 0; start < remotes.length; start += parallelAgents) {
			const batch = remotes.slice(start, start + parallelAgents).map(([index, entry]) => runRow(index, entry));
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
		class="flex max-h-(--max-height-dscreen-90) flex-col overflow-hidden sm:max-w-200"
	>
		{#if phase === 'confirm'}
			<div class="px-6 py-4">
				<Dialog.Header>
					<div class="space-y-3">
						<Dialog.Title>{title}</Dialog.Title>
						{#if versionInformation}
							<VersionUpdateSummary {versionInformation} {releasedAgo} />
						{/if}
					</div>
				</Dialog.Header>
			</div>

			{#if planEnvironments.length > 0}
				<div class="shrink-0 border-t border-border/60">
					<button
						type="button"
						class="flex w-full items-center gap-3 px-6 py-3 text-left text-sm"
						aria-expanded={planOpen}
						onclick={() => (foldToggles = { ...foldToggles, plan: !planOpen })}
					>
						<span class="font-medium">{m.all_environments_count({ count: planEnvironments.length })}</span>
						{#if planOfflineCount > 0}
							<span class="text-xs text-warning">{m.environments_update_all_legend_skipped({ count: planOfflineCount })}</span>
						{/if}
						<ArrowRightIcon class={cn('ml-auto size-3.5 text-muted-foreground transition-transform', planOpen && 'rotate-90')} />
					</button>
					{#if planOpen}
						<div class="max-h-56 overflow-y-auto px-6 pb-1" transition:slide={{ duration: 200 }}>
							<table class="w-full text-sm">
								<thead>
									<tr class="text-left text-xs text-muted-foreground">
										<th class="pb-2 font-normal">{m.agent()}</th>
										<th class="pb-2 text-right font-normal">{m.common_status()}</th>
									</tr>
								</thead>
								<tbody>
									{#each planEnvironments as env (env.id)}
										<tr class="border-t border-border/50">
											<td class="py-2.5">
												<span class="flex items-center gap-1.5">
													<span class="truncate">{env.name}</span>
													{#if env.id === MANAGER_ENVIRONMENT_ID}
														<span class="shrink-0 rounded-xl border px-1 text-3xs text-muted-foreground">{m.manager()}</span>
													{/if}
												</span>
											</td>
											<td class="py-2.5 text-right text-xs">
												{#if isEnvironmentOnline(env)}
													<span class="text-muted-foreground">{m.common_online()}</span>
												{:else}
													<span class="text-warning">{m.environments_update_all_status_skipped_offline()}</span>
												{/if}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</div>
			{/if}

			{#if versionInformation}
				<div class="flex min-h-0 flex-1 flex-col border-t border-border/60">
					<div class="flex items-center justify-between px-6 pt-4 pb-2">
						<h3 class="text-sm font-semibold text-foreground">{m.update_center_whats_new()}</h3>
						{#if releaseUrl}
							<a
								href={releaseUrl}
								target="_blank"
								rel="external noopener noreferrer"
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
			<div class="px-6 py-4">
				<Dialog.Header>
					<div class="flex items-center justify-between gap-3">
						<div class="flex min-w-0 items-center gap-2">
							{#if phase === 'finished'}
								{#if failed}
									<AlertIcon class="size-4 shrink-0 text-destructive" />
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
				<div class="flex min-h-0 flex-col border-t border-border/60 sm:h-104 sm:flex-row">
					<!-- Native scroller: ScrollArea's percentage-height viewport resolves to auto
					     under a max-height-only parent, so it never clips (#3655). -->
					<div
						class="max-h-48 shrink-0 overflow-y-auto border-b border-border/60 p-2 sm:max-h-none sm:w-60 sm:border-r sm:border-b-0"
					>
						<ul>
							{#each queueGroups as group (group.key)}
								{#if group.results.length > 0}
									<li>
										{#if group.foldable}
											<button
												type="button"
												class="flex w-full items-center justify-between rounded-xl px-2.5 pt-2 pb-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
												aria-expanded={group.open}
												onclick={() => (foldToggles = { ...foldToggles, [group.key]: !group.open })}
											>
												{group.label({ count: group.results.length })}
												<ArrowRightIcon class={cn('size-3.5 transition-transform', group.open && 'rotate-90')} />
											</button>
										{:else}
											<span class="block px-2.5 pt-2 pb-1 text-xs text-muted-foreground tabular-nums">
												{group.label({ count: group.results.length })}
											</span>
										{/if}
										{#if group.open}
											<ul transition:slide={{ duration: 200 }}>
												{#each group.results as result (result.environmentId)}
													<UpgradeQueueRow
														{result}
														selected={result.environmentId === selectedResult?.environmentId}
														onselect={() => (pinnedId = result.environmentId)}
													/>
												{/each}
											</ul>
										{/if}
									</li>
								{/if}
							{/each}
						</ul>
					</div>
					<div class="min-h-0 min-w-0 flex-1 overflow-y-auto">
						{#if selectedResult}
							<UpgradeJobDetail result={selectedResult} />
						{/if}
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
				<p class="text-xs text-muted-foreground sm:mr-auto sm:self-center">{m.manager_updates_last()}</p>
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
