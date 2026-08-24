<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog';
	import * as ScrollArea from '#lib/components/ui/scroll-area';
	import { Button } from '#lib/components/ui/button';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { cn } from '#lib/utils';
	import { m } from '#lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { onDestroy } from 'svelte';
	import { refreshAll } from '$app/navigation';
	import systemUpgradeService, {
		type UpdateAllJob,
		type UpdateAllEnvironmentResult,
		type UpdateAllEnvironmentStatus
	} from '#lib/services/api/system-upgrade-service';
	import { SuccessIcon, ClockIcon, AlertIcon, AlertTriangleIcon, ExternalLinkIcon } from '#lib/icons';
	import BaseAPIService from '#lib/services/api-service';
	import ReleaseNotes from '#lib/components/release-notes.svelte';
	import type { AppVersionInformation } from '#lib/types/settings';
	import { formatDistanceToNow } from 'date-fns';
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
	const MANAGER_ENVIRONMENT_ID = '0';

	let phase = $state<Phase>('confirm');
	let job = $state<UpdateAllJob | null>(null);
	let reconnecting = $state(false);
	let pollActive = false;
	let pollTimer: ReturnType<typeof setTimeout> | null = null;

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
		try {
			const next = await systemUpgradeService.getUpdateAllStatus();
			reconnecting = false;
			job = next;
			if (next.status === 'completed' || next.status === 'failed') {
				stopPolling();
				finishTerminalJob(next);
				return;
			}
		} catch {
			// The manager is likely restarting after its own upgrade — keep retrying
			// until the backend answers again.
			reconnecting = true;
		}
		schedulePoll();
	}

	async function handleConfirm() {
		phase = 'running';
		reconnecting = false;

		if (debugDemo) {
			void runDebugDemo();
			return;
		}

		BaseAPIService.setUpgradeInProgress(true);
		try {
			job = await systemUpgradeService.triggerUpdateAll();
		} catch {
			toast.error(m.environments_update_all_trigger_failed());
			resetState();
			return;
		}

		if (phase !== 'running') return;

		if (job && (job.status === 'completed' || job.status === 'failed')) {
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
			try {
				await refreshAll();
			} catch {
				// A failed refresh must not trap the dialog open.
			}
		}
		resetState();
		open = false;
		await onFinished?.();
	}

	onDestroy(() => {
		stopPolling();
		BaseAPIService.setUpgradeInProgress(false);
	});

	// Version/release presentation for the confirm step, rendered when the caller
	// has the manager's version information at hand (sidebar / mobile nav).
	const releaseNotes = $derived(versionInformation?.releaseNotes?.trim() ?? '');
	const releaseUrl = $derived(versionInformation?.releaseUrl ?? '');
	const releasedAgo = $derived.by(() => {
		const at = versionInformation?.releasedAt;
		if (!at) return '';
		const date = new Date(at);
		if (Number.isNaN(date.getTime())) return '';
		return formatDistanceToNow(date, { addSuffix: true });
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

	function statusLabel(status: UpdateAllEnvironmentStatus): string {
		switch (status) {
			case 'updating':
				return m.common_action_updating();
			case 'updated':
				return m.common_updated();
			case 'up_to_date':
				return m.image_update_up_to_date_title();
			case 'triggered':
				return m.environments_update_all_status_triggered();
			case 'skipped_offline':
				return m.environments_update_all_status_skipped_offline();
			case 'failed':
				return m.common_failed();
			default:
				return m.common_pending();
		}
	}

	// One segment per environment, colored by outcome: the whole fleet's state reads
	// at a glance, and an in-flight environment stays indeterminate because the
	// backend reports status transitions, never a percentage.
	function segmentClass(status: UpdateAllEnvironmentStatus): string {
		switch (status) {
			case 'updating':
				return 'bg-primary animate-pulse';
			case 'updated':
			case 'triggered':
			case 'up_to_date':
				return 'bg-green-500';
			case 'skipped_offline':
				return 'bg-amber-500';
			case 'failed':
				return 'bg-destructive';
			default:
				return 'bg-muted';
		}
	}

	function badgeClass(status: UpdateAllEnvironmentStatus): string {
		switch (status) {
			case 'updated':
			case 'triggered':
			case 'up_to_date':
				return 'border-green-500/40 bg-green-500/10 text-green-600 dark:text-green-400';
			case 'updating':
				return 'border-primary/40 bg-primary/10 text-primary';
			case 'skipped_offline':
				return 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400';
			case 'failed':
				return 'border-destructive/40 bg-destructive/10 text-destructive';
			default:
				return 'border-border bg-muted/40 text-muted-foreground/60';
		}
	}

	function labelClass(status: UpdateAllEnvironmentStatus): string {
		switch (status) {
			case 'updating':
				return 'text-primary';
			case 'skipped_offline':
				return 'text-amber-600 dark:text-amber-400';
			case 'failed':
				return 'text-destructive';
			default:
				return 'text-muted-foreground';
		}
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
			createdAt: new Date().toISOString(),
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
		job.completedAt = new Date().toISOString();
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
		class={cn('gap-0 overflow-hidden p-0 sm:max-w-[520px]', phase === 'running' && '[&>button]:hidden')}
		onInteractOutside={(e: Event) => {
			if (phase === 'running') e.preventDefault();
		}}
	>
		{#if phase === 'confirm'}
			<div class="px-6 pt-6 pb-4">
				<Dialog.Header class="space-y-3">
					<Dialog.Title class="text-lg">{title}</Dialog.Title>
					<Dialog.Description>{m.environments_update_all_message()}</Dialog.Description>
					{#if versionInformation}
						<VersionUpdateSummary {versionInformation} {releasedAgo} />
					{/if}
				</Dialog.Header>
			</div>

			{#if versionInformation}
				<div class="border-t border-border/60">
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
					<ScrollArea.Root class="h-[220px] px-6 pb-4">
						{#if releaseNotes}
							<ReleaseNotes markdown={releaseNotes} />
						{:else}
							<p class="text-sm text-muted-foreground italic">{m.update_center_release_notes_unavailable()}</p>
						{/if}
					</ScrollArea.Root>
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
									<SuccessIcon class="size-4 shrink-0 text-green-600 dark:text-green-400" />
								{/if}
							{/if}
							<Dialog.Title class="truncate text-lg">{title}</Dialog.Title>
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
							<div class={cn('h-1.5 flex-1 rounded-full transition-colors', segmentClass(result.status))}></div>
						{/each}
					</div>
				{/if}

				{#if reconnecting}
					<div
						class="mt-4 flex items-center gap-2 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-400"
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
										class={cn('flex size-7 shrink-0 items-center justify-center rounded-full border', badgeClass(result.status))}
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
												<span class="shrink-0 rounded border px-1 text-[10px] text-muted-foreground">{m.manager()}</span>
											{/if}
										</div>
										{#if result.status === 'failed' && result.error}
											<span class="block truncate text-xs text-muted-foreground" title={result.error}>{result.error}</span>
										{:else if versions}
											<span class="block truncate text-xs text-muted-foreground tabular-nums">{versions}</span>
										{/if}
									</div>

									<span class={cn('shrink-0 text-xs', labelClass(result.status))}>
										{statusLabel(result.status)}
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

		<Dialog.Footer class="border-t border-border/60 px-6 py-4">
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
