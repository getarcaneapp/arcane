<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';

	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages.js';
	import { onDestroy } from 'svelte';
	import { refreshAll } from '$app/navigation';
	import systemUpgradeService, {
		type UpdateAllJob,
		type UpdateAllEnvironmentResult,
		type UpdateAllEnvironmentStatus
	} from '#lib/services/api/system-upgrade-service.js';
	import { SuccessIcon, ClockIcon, AlertIcon, AlertTriangleIcon, ExternalLinkIcon } from '#lib/icons/index.js';
	import BaseAPIService, { APIError } from '#lib/services/api-service.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import type { AppVersionInformation } from '#lib/types/settings.js';
	import { formatRelativeTime, nowInstantString } from '#lib/utils/formatting.js';
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

	// Reset on close (not on open) so a reopened dialog always starts at the confirm
	// step, without mutating $state from inside an $effect. The confirm step never
	// renders job/reconnecting, so clearing them here is safe.
	function resetState() {
		startAttempt++;
		stopPolling();
		BaseAPIService.setUpgradeInProgress(false);
		phase = 'confirm';
		job = null;
		reconnecting = false;
	}

	function schedulePoll() {
		if (!pollActive) return;
		pollTimer = setTimeout(poll, POLL_INTERVAL_MS);
	}

	function finishTerminalJob(terminalJob: UpdateAllJob) {
		const managerRestarted =
			terminalJob.results?.some((result) => result.environmentId === MANAGER_ENVIRONMENT_ID && result.status === 'updated') ??
			false;
		// Keep the upgrade flag armed when the manager actually restarted: the refresh in
		// handleClose may hit the new backend with a stale token, and api-service only
		// recovers that version-mismatch 401 while the flag is set.
		if (!managerRestarted) {
			BaseAPIService.setUpgradeInProgress(false);
		}
		phase = 'finished';
	}

	async function poll() {
		if (!pollActive) return;

		const requestResult1 = await tryCatch(
			(async () => {
				const next = await systemUpgradeService.getUpdateAllStatus();
				reconnecting = false;
				job = next;
				if (next.status === 'completed' || next.status === 'failed') {
					stopPolling();
					finishTerminalJob(next);
					return true;
				}
			})()
		);
		if (requestResult1.error !== null) {
			// The manager is likely restarting after its own upgrade — keep retrying
			// until the backend answers again.
			reconnecting = true;
		}
		if (requestResult1.data) return;

		schedulePoll();
	}

	async function handleConfirm() {
		const attempt = ++startAttempt;
		phase = 'running';
		reconnecting = false;

		if (debugDemo) {
			void runDebugDemo();
			return;
		}

		BaseAPIService.setUpgradeInProgress(true);

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
				if (active?.status === 'running' || active?.status === 'pending_restart') {
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

		job = next;
		if (job.status === 'completed' || job.status === 'failed') {
			finishTerminalJob(job);
			return;
		}

		pollActive = true;
		schedulePoll();
	}

	async function handleClose() {
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
		resetState();
		open = false;
		await onFinished?.();
	}

	onDestroy(() => {
		startAttempt++;
		stopPolling();
		BaseAPIService.setUpgradeInProgress(false);
	});

	// Version/release presentation for the confirm step, rendered when the caller
	// has the manager's version information at hand (sidebar / mobile nav).
	const releaseNotes = $derived(versionInformation?.releaseNotes?.trim() ?? '');
	const releaseUrl = $derived(versionInformation?.releaseUrl ?? '');
	const releasedAgo = $derived.by(() => {
		const at = versionInformation?.releasedAt;
		return at ? formatRelativeTime(at) : '';
	});

	const title = $derived.by(() => {
		if (phase === 'confirm') return m.environments_update_all_title();
		if (phase === 'finished') {
			return job?.status === 'failed' ? m.environments_update_all_failed() : m.environments_update_all_completed();
		}
		return m.environments_update_all_in_progress();
	});

	// "Done" is every environment that has reached a terminal state — i.e. anything
	// that isn't still queued (pending) or actively being worked on (updating).
	const results = $derived(job?.results ?? []);
	const totalCount = $derived(results.length);
	const doneCount = $derived(results.filter((r) => r.status !== 'pending' && r.status !== 'updating').length);
	const failed = $derived(job?.status === 'failed');

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
			triggered: { label: m.environments_update_all_status_triggered, ...completedColors },
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
		const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
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
			const starting = job?.results?.[index];
			if (phase !== 'running' || !starting) return;
			starting.status = 'updating';
			await wait(700);

			// The manager restart is the one moment the reconnecting banner shows.
			if (index === 0) {
				reconnecting = true;
				await wait(1600);
				reconnecting = false;
			}

			const row = job?.results?.[index];
			if (phase !== 'running' || !row) return;
			row.status = entry.outcome;
			row.toVersion = entry.to;
			if (entry.error) row.error = entry.error;
		}

		if (phase !== 'running' || !job) return;
		job.status = 'completed';
		job.completedAt = nowInstantString();
		phase = 'finished';
	}
</script>

<Dialog.Root
	{open}
	onOpenChange={(next) => {
		if (!next) {
			resetState();
		}
		open = next;
	}}
>
	<Dialog.Content
		sectioned
		class={cn(
			'flex max-h-(--max-height-dscreen-90) flex-col overflow-hidden sm:max-w-130',
			phase === 'running' && '[&>button]:hidden'
		)}
		onInteractOutside={(e: Event) => {
			if (phase === 'running') e.preventDefault();
		}}
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
							<span class="shrink-0 text-xs text-muted-foreground tabular-nums">
								{m.environments_update_all_progress({ done: doneCount, total: totalCount })}
							</span>
						{/if}
					</div>
				</Dialog.Header>

				{#if totalCount > 0}
					<div class="mt-3 flex gap-1">
						{#each results as result (result.environmentId)}
							<div class={cn('h-1.5 flex-1 rounded-full transition-colors', statusPresentation(result.status).segment)}></div>
						{/each}
					</div>
				{/if}

				{#if reconnecting}
					<div
						class="mt-4 flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/5 px-3 py-2.5 text-sm text-warning"
					>
						<Spinner class="size-4" />
						<span>{m.environments_update_all_manager_restarting()}</span>
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
										{#if result.status === 'updated' || result.status === 'triggered' || result.status === 'up_to_date'}
											<SuccessIcon class="size-3.5" />
										{:else if result.status === 'updating'}
											<Spinner class="size-3.5" />
										{:else if result.status === 'skipped_offline'}
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

									<span class={cn('shrink-0 text-xs', statusPresentation(result.status).text)}>
										{statusPresentation(result.status).label()}
									</span>
								</li>
							{/each}
						</ul>
					</div>
				</div>
			{:else}
				<div class="flex items-center gap-2 border-t border-border/60 px-6 py-4 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					<span>{m.environments_update_all_in_progress()}</span>
				</div>
			{/if}

			{#if phase === 'running'}
				<div class="border-t border-border/60 bg-muted/30 px-6 py-3">
					<p class="text-xs leading-relaxed text-muted-foreground">{m.environments_update_all_manager_note()}</p>
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
