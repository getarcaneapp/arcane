<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import { onDestroy } from 'svelte';
	import type { Attachment } from 'svelte/attachments';

	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Popover from '#lib/components/ui/popover/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, ClockIcon, AlertIcon, AlertTriangleIcon, ExternalLinkIcon, InfoIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { APIError } from '#lib/services/api-service.js';
	import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
	import type { AppVersionInformation } from '#lib/types/settings.js';
	import type {
		UpdateAllJob,
		UpdateAllEnvironmentResult,
		UpdateAllEnvironmentStatus,
		UpdateAllStage
	} from '#lib/types/system-upgrade.js';
	import { cn } from '#lib/utils.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { formatElapsedTime, formatRelativeTime, nowInstantString } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

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
		/** Dev-only: run a scripted fake fleet instead of calling the API. */
		debugDemo?: boolean;
		onFinished?: () => void | Promise<void>;
	} = $props();

	type Phase = 'confirm' | 'running' | 'finished';

	const POLL_INTERVAL_MS = 3000;
	// After a 409 the winning job may not be persisted yet; re-read status this many
	// times, this far apart, before concluding there is nothing to adopt.
	const CONFLICT_STATUS_READS = 3;
	const CONFLICT_STATUS_RETRY_MS = 1000;
	const MANAGER_ENVIRONMENT_ID = '0';

	let phase = $state<Phase>('confirm');
	let job = $state<UpdateAllJob | null>(null);
	let reconnecting = $state(false);
	let progressError = $state('');
	// Ticks once a second while the dialog is open so elapsed stage times advance.
	let clock = $state<string>();
	let pollActive = false;
	let pollTimer: ReturnType<typeof setTimeout> | null = null;
	// Bumped on every confirm and reset so a startup/status response that lands after
	// the dialog closed (or a newer attempt began) is ignored instead of restarting
	// polling for a dialog nobody is looking at.
	let startAttempt = 0;

	function stopPolling() {
		pollActive = false;
		if (pollTimer) {
			clearTimeout(pollTimer);
			pollTimer = null;
		}
	}

	// Closing stops observing; the backend job and restart recovery keep running.
	function resetState() {
		startAttempt++;
		stopPolling();
		phase = 'confirm';
		job = null;
		reconnecting = false;
		progressError = '';
	}

	function schedulePoll() {
		if (!pollActive) return;
		pollTimer = setTimeout(poll, POLL_INTERVAL_MS);
	}

	function isActiveJob(candidate: UpdateAllJob | null | undefined): candidate is UpdateAllJob {
		return candidate?.status === 'running' || candidate?.status === 'pending_restart';
	}

	function followJob(next: UpdateAllJob) {
		job = next;
		reconnecting = false;
		progressError = '';
		if (!isActiveJob(next)) {
			stopPolling();
			phase = 'finished';
			return;
		}
		phase = 'running';
		if (!debugDemo) systemUpgradeService.monitorUpdateAllRecovery(next);
		pollActive = true;
		schedulePoll();
	}

	// A status read that never reached the backend, or hit a gateway with no backend
	// behind it, is what a manager restart looks like from the browser.
	function isConnectionLost(error: unknown): boolean {
		if (error instanceof TypeError) return true;
		if (!(error instanceof APIError)) return false;
		return !error.status || [502, 503, 504].includes(error.status);
	}

	function handleStatusError(error: unknown) {
		// No job has ever run: nothing to follow.
		if (!job && error instanceof APIError && error.status === 404) {
			stopPolling();
			phase = 'confirm';
			progressError = '';
			return;
		}
		reconnecting = job?.status === 'pending_restart' && isConnectionLost(error);
		progressError = reconnecting ? '' : extractApiErrorMessage(error);
		pollActive = true;
		schedulePoll();
	}

	async function poll() {
		if (!pollActive) return;
		const attempt = startAttempt;
		const response = await tryCatch(systemUpgradeService.getUpdateAllStatus());
		if (!pollActive || attempt !== startAttempt) return;
		if (response.error !== null) {
			handleStatusError(response.error);
			return;
		}
		followJob(response.data);
	}

	// Reopening the dialog picks up an update that is still running on the backend.
	async function restoreActiveJob() {
		const attempt = ++startAttempt;
		phase = 'running';
		const response = await tryCatch(systemUpgradeService.getUpdateAllStatus());
		if (attempt !== startAttempt) return;
		if (response.error !== null) {
			handleStatusError(response.error);
			return;
		}
		if (isActiveJob(response.data)) {
			followJob(response.data);
		} else {
			phase = 'confirm';
		}
	}

	// Dialog.Content only exists while the dialog is open, so this runs once per
	// opening and its cleanup once per close.
	const observeUpdate: Attachment = () => {
		if (!debugDemo) void restoreActiveJob();
		const ticker = setInterval(() => (clock = nowInstantString()), 1000);
		return () => {
			clearInterval(ticker);
			resetState();
		};
	};

	async function handleConfirm() {
		const attempt = ++startAttempt;
		phase = 'running';
		reconnecting = false;
		progressError = '';

		if (debugDemo) {
			void runDebugDemo();
			return;
		}

		const startResult = await tryCatch(systemUpgradeService.triggerUpdateAll());
		let next = startResult.data;
		// The backend refuses a second job while one is active (409). Adopt that job
		// and follow it instead of failing; never retry the POST.
		if (startResult.error instanceof APIError && startResult.error.status === 409) {
			// A concurrent start answers 409 before its job row is persisted, so the first
			// read can still show no job (404) or the previous terminal one.
			for (let read = 0; read < CONFLICT_STATUS_READS && attempt === startAttempt; read++) {
				if (read > 0) await new Promise((resolve) => setTimeout(resolve, CONFLICT_STATUS_RETRY_MS));
				const active = (await tryCatch(systemUpgradeService.getUpdateAllStatus())).data;
				if (isActiveJob(active)) {
					next = active;
					break;
				}
			}
		}
		// The dialog closed (or was re-confirmed) while the request was in flight.
		if (attempt !== startAttempt) return;

		if (!next) {
			await handleApiResultWithCallbacks({ result: startResult, message: m.environments_update_all_trigger_failed() });
			resetState();
			return;
		}

		followJob(next);
	}

	async function handleClose() {
		const attempt = ++startAttempt;
		stopPolling();
		// Refresh through SvelteKit instead of reloading the document: a hard reload
		// lands while agents are still reconnecting after the fleet restart, which
		// renders every environment as offline for a moment.
		if (job && !debugDemo) {
			const operationResult2 = await tryCatch(
				(async () => {
					await refreshAll();
				})()
			);
			if (operationResult2.error !== null) {
				// A failed refresh must not trap the dialog open.
			}
		}
		if (attempt !== startAttempt) return;
		resetState();
		open = false;
		await onFinished?.();
	}

	onDestroy(() => {
		startAttempt++;
		stopPolling();
	});

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

	const completedColors = {
		segment: 'bg-success',
		badge: 'border-success/40 bg-success/10 text-success',
		text: 'text-muted-foreground'
	};
	const pendingDisplay = {
		label: m.common_pending,
		segment: 'bg-muted',
		badge: 'border-border bg-muted/40 text-muted-foreground/60',
		text: 'text-muted-foreground'
	};
	const environmentStatusDisplay = new Map(
		Object.entries({
			pending: pendingDisplay,
			updating: {
				label: m.common_action_updating,
				segment: 'bg-primary animate-pulse',
				badge: 'border-primary/40 bg-primary/10 text-primary',
				text: 'text-primary'
			},
			updated: { label: m.common_updated, ...completedColors },
			up_to_date: { label: m.image_update_up_to_date_title, ...completedColors },
			triggered: {
				label: m.environments_update_all_status_triggered,
				segment: 'bg-warning',
				badge: 'border-warning/40 bg-warning/10 text-warning',
				text: 'text-warning'
			},
			skipped_offline: {
				label: m.environments_update_all_status_skipped_offline,
				segment: 'bg-warning',
				badge: 'border-warning/40 bg-warning/10 text-warning',
				text: 'text-warning'
			},
			failed: {
				label: m.common_failed,
				segment: 'bg-destructive',
				badge: 'border-destructive/40 bg-destructive/10 text-destructive',
				text: 'text-destructive'
			}
		} satisfies Record<UpdateAllEnvironmentStatus, { label: () => string; segment: string; badge: string; text: string }>)
	);

	function statusPresentation(status: UpdateAllEnvironmentStatus) {
		return environmentStatusDisplay.get(status) ?? pendingDisplay;
	}

	const stageLabels: Record<UpdateAllStage, () => string> = {
		checking: m.environments_update_all_stage_checking,
		starting: m.environments_update_all_stage_starting,
		reconnecting: m.environments_update_all_stage_reconnecting,
		verifying: m.environments_update_all_stage_verifying
	};

	// While a row is updating its stage replaces the generic status label.
	function rowLabel(result: UpdateAllEnvironmentResult): string {
		const statusLabel = statusPresentation(result.status).label;
		if (result.status !== 'updating' || !result.stage) return statusLabel();
		if (result.stage === 'reconnecting' && result.environmentId === MANAGER_ENVIRONMENT_ID) {
			return m.environments_update_all_stage_manager_restart();
		}
		return (stageLabels[result.stage] ?? statusLabel)();
	}

	// An environment that was already current reports the same version twice; show it
	// once rather than as a "v1.0.0 → v1.0.0" no-op transition.
	function versionLine(result: UpdateAllEnvironmentResult): string {
		const from = result.fromVersion?.trim() ?? '';
		const to = result.toVersion?.trim() ?? '';
		if (!from || !to || from === to) return to || from;
		return `${from} → ${to}`;
	}

	// Dev-only preview: step a fake fleet through every row state so this dialog can be
	// designed and reviewed without triggering a real fleet update. Only ever enabled
	// from a dev-guarded callsite.
	async function runDebugDemo() {
		const attempt = startAttempt;
		const live = () => attempt === startAttempt && phase === 'running';
		// Sleeps, then reports whether the demo is still the one being watched.
		const pause = async (ms: number) => {
			await new Promise((resolve) => setTimeout(resolve, ms));
			return live();
		};
		const enterStage = (row: UpdateAllEnvironmentResult, stage: UpdateAllStage) => {
			row.stage = stage;
			row.stageStartedAt = nowInstantString();
		};
		// Big enough fleet (10) to exercise the scrolling list (#3655).
		const demo: Array<{ name: string; outcome: UpdateAllEnvironmentStatus; to: string; error?: string }> = [
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

		job = {
			id: 'demo',
			status: 'running',
			createdAt: nowInstantString(),
			results: demo.map((entry, index) => ({
				environmentId: index === 0 ? MANAGER_ENVIRONMENT_ID : `demo-${index}`,
				environmentName: entry.name,
				status: 'pending',
				fromVersion: 'v0.9.1'
			}))
		};

		// Remotes first, manager last — the real processing order.
		const steps = [...demo.entries()].map(([index, entry]) => ({ index, entry }));
		for (const { index, entry } of [...steps.slice(1), ...steps.slice(0, 1)]) {
			const row = job?.results?.[index];
			if (!live() || !row) return;
			row.status = 'updating';
			enterStage(row, 'checking');
			if (!(await pause(700))) return;
			enterStage(row, 'starting');
			if (!(await pause(700))) return;
			enterStage(row, 'reconnecting');
			// One slow reconnect and one status-read outage, to exercise both displays.
			if (index === 1 && !(await pause(5000))) return;
			if (index === 2) {
				progressError = m.environments_update_all_progress_unavailable();
				if (!(await pause(2000))) return;
				progressError = '';
			}
			// The manager restart is the one moment the reconnecting banner shows.
			if (index === 0 && job) {
				job.status = 'pending_restart';
				reconnecting = true;
				if (!(await pause(3000))) return;
				reconnecting = false;
			}
			enterStage(row, 'verifying');
			if (!(await pause(700))) return;
			row.status = entry.outcome;
			row.stage = undefined;
			row.stageStartedAt = undefined;
			row.toVersion = entry.to;
			if (entry.error) row.error = entry.error;
		}

		if (!live() || !job) return;
		job.status = 'completed';
		job.completedAt = nowInstantString();
		phase = 'finished';
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
						<div class="flex shrink-0 items-center gap-1 pr-6">
							{#if totalCount > 0}
								<span class="text-xs text-muted-foreground tabular-nums">
									{m.environments_update_all_progress({ done: doneCount, total: totalCount })}
								</span>
							{/if}
							{#if phase === 'running'}
								<Popover.Root>
									<Popover.Trigger>
										{#snippet child({ props })}
											<Button {...props} variant="ghost" size="icon" class="size-7" aria-label={m.info()}>
												<InfoIcon class="size-4" />
											</Button>
										{/snippet}
									</Popover.Trigger>
									<Popover.Content align="end" class="w-80">
										<div class="space-y-2 text-xs leading-relaxed text-muted-foreground">
											<p>{m.environments_update_all_manager_note()}</p>
											<p>{m.environments_update_all_reconnection_note()}</p>
										</div>
									</Popover.Content>
								</Popover.Root>
							{/if}
						</div>
					</div>
				</Dialog.Header>

				{#if totalCount > 0}
					<div class="mt-3 flex gap-1">
						{#each results as result (result.environmentId)}
							<div class={cn('h-1.5 flex-1 rounded-full transition-colors', statusPresentation(result.status).segment)}></div>
						{/each}
					</div>
				{/if}

				{#if phase === 'finished'}
					<p class="mt-3 text-sm text-muted-foreground">{m.environments_update_all_summary(outcomeCounts)}</p>
					{#if job?.error}<p class="mt-2 text-sm text-destructive">{job.error}</p>{/if}
				{/if}
				{#if progressError}
					<div role="status" class="mt-4 rounded-lg border border-warning/20 bg-warning/5 px-3 py-2.5 text-sm">
						<p class="font-medium">{m.environments_update_all_progress_unavailable()}</p>
						<p class="mt-1 break-words text-muted-foreground">{progressError}</p>
					</div>
				{/if}

				{#if reconnecting}
					<div
						class="mt-4 flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/5 px-3 py-2.5 text-sm text-warning"
					>
						<Spinner class="size-4" />
						<div>
							<span>{m.environments_update_all_manager_restarting()}</span>
							<span class="block text-xs">
								{m.environments_update_all_elapsed({
									duration: formatElapsedTime(managerResult?.stageStartedAt ?? job?.createdAt, { base: clock })
								})}
							</span>
						</div>
					</div>
				{/if}
			</div>

			{#if totalCount > 0}
				<div class="border-t border-border/60">
					<!-- Native scroller: ScrollArea's percentage-height viewport resolves to auto
					     under a max-height-only parent, so it never clips (#3655). -->
					<div class="max-h-72 overflow-y-auto">
						<ul class="divide-y divide-border/50">
							{#each results as result (result.environmentId)}
								{@const versions = versionLine(result)}
								<li class="flex items-center gap-3 px-6 py-2.5 text-sm">
									<span
										class={cn(
											'flex size-7 shrink-0 items-center justify-center rounded-full border',
											statusPresentation(result.status).badge
										)}
									>
										{#if result.status === 'updated' || result.status === 'up_to_date'}
											<SuccessIcon class="size-3.5" />
										{:else if result.status === 'updating'}
											<Spinner class="size-3.5" />
										{:else if result.status === 'skipped_offline' || result.status === 'triggered'}
											<AlertIcon class="size-3.5" />
										{:else if result.status === 'failed'}
											<AlertTriangleIcon class="size-3.5" />
										{:else}
											<ClockIcon class="size-3.5" />
										{/if}
									</span>

									<div class="min-w-0 flex-1">
										<div class="flex items-center gap-1.5">
											<span class="truncate font-medium">{result.environmentName}</span>
											{#if result.environmentId === MANAGER_ENVIRONMENT_ID}
												<span class="shrink-0 rounded border px-1 text-3xs text-muted-foreground">{m.manager()}</span>
											{/if}
										</div>
										{#if result.status === 'failed' && result.error}
											<span class="block truncate text-xs text-muted-foreground" title={result.error}>{result.error}</span>
										{:else if versions}
											<span class="block truncate text-xs text-muted-foreground tabular-nums">{versions}</span>
										{/if}
									</div>

									<div class={cn('max-w-48 shrink-0 text-right text-xs', statusPresentation(result.status).text)}>
										<span>{rowLabel(result)}</span>
										{#if result.status === 'updating' && result.stageStartedAt}
											<span class="block text-muted-foreground">
												{m.environments_update_all_elapsed({
													duration: formatElapsedTime(result.stageStartedAt, { base: clock })
												})}
											</span>
										{/if}
									</div>
								</li>
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
					<Button onclick={handleConfirm}>{m.update_all()}</Button>
				{/if}
			{:else}
				<Button variant="outline" onclick={handleClose}>{m.common_close()}</Button>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
