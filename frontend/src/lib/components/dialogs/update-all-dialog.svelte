<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as ScrollArea from '$lib/components/ui/scroll-area';
	import { Button } from '$lib/components/ui/button';
	import Spinner from '$lib/components/ui/spinner/spinner.svelte';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { onDestroy } from 'svelte';
	import systemUpgradeService, {
		type UpdateAllJob,
		type UpdateAllEnvironmentStatus
	} from '$lib/services/api/system-upgrade-service';
	import { SuccessIcon, ClockIcon, AlertIcon, AlertTriangleIcon, ExternalLinkIcon } from '$lib/icons';
	import BaseAPIService from '$lib/services/api-service';
	import ReleaseNotes from '$lib/components/release-notes.svelte';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { formatDistanceToNow } from 'date-fns';
	import VersionUpdateSummary from './version-update-summary.svelte';

	// open has no $bindable fallback: upstream binds can start out undefined, and
	// binding undefined to a $bindable with a fallback throws props_invalid_value.
	let {
		open = $bindable(undefined),
		versionInformation,
		canConfirm = true,
		onFinished
	}: {
		open?: boolean;
		versionInformation?: AppVersionInformation;
		canConfirm?: boolean;
		onFinished?: () => void | Promise<void>;
	} = $props();

	type Phase = 'confirm' | 'running' | 'finished';

	const POLL_INTERVAL_MS = 3000;

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
		const managerUpdated =
			terminalJob.status === 'completed' &&
			(terminalJob.results?.some((result) => result.environmentId === '0' && result.status === 'updated') ?? false);
		if (managerUpdated) {
			window.location.reload();
			return;
		}
		BaseAPIService.setUpgradeInProgress(false);
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
	const totalCount = $derived(job?.results?.length ?? 0);
	const doneCount = $derived(job?.results?.filter((r) => r.status !== 'pending' && r.status !== 'updating').length ?? 0);
	const progressPct = $derived(totalCount > 0 ? Math.round((doneCount / totalCount) * 100) : 0);

	function statusLabel(status: UpdateAllEnvironmentStatus): string {
		switch (status) {
			case 'updating':
				return m.common_action_updating();
			case 'updated':
				return m.common_updated();
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
		class="sm:max-w-[520px]"
		onInteractOutside={(e: Event) => {
			if (phase === 'running') e.preventDefault();
		}}
	>
		<Dialog.Header>
			<Dialog.Title>{title}</Dialog.Title>
			{#if phase === 'confirm'}
				<Dialog.Description>{m.environments_update_all_message()}</Dialog.Description>
			{/if}
		</Dialog.Header>

		{#if phase === 'confirm' && versionInformation}
			<div class="space-y-4">
				<VersionUpdateSummary {versionInformation} {releasedAgo} />

				<div class="border-t border-border/60 pt-3">
					<div class="flex items-center justify-between pb-2">
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
					<ScrollArea.Root class="h-[220px]">
						{#if releaseNotes}
							<ReleaseNotes markdown={releaseNotes} />
						{:else}
							<p class="text-sm text-muted-foreground italic">{m.update_center_release_notes_unavailable()}</p>
						{/if}
					</ScrollArea.Root>
				</div>
			</div>
		{/if}

		{#if phase !== 'confirm'}
			<div class="space-y-3">
				{#if reconnecting}
					<div class="flex items-center gap-2 text-sm text-muted-foreground">
						<Spinner class="size-4" />
						<span>{m.environments_update_all_manager_restarting()}</span>
					</div>
				{/if}

				{#if job?.results?.length}
					{#if phase === 'running'}
						<div class="space-y-1.5">
							<div class="flex items-center justify-end text-xs text-muted-foreground">
								<span>{m.environments_update_all_progress({ done: doneCount, total: totalCount })}</span>
							</div>
							<div class="h-1.5 overflow-hidden rounded-full bg-muted">
								<div class="h-full rounded-full bg-primary transition-all duration-500" style="width: {progressPct}%"></div>
							</div>
						</div>
					{/if}

					<ScrollArea.Root class="max-h-72">
						<ul class="divide-y divide-border">
							{#each job.results as result (result.environmentId)}
								<li class="flex items-center gap-3 py-2.5 text-sm">
									<span
										class={cn(
											'flex size-7 shrink-0 items-center justify-center rounded-full border',
											(result.status === 'updated' || result.status === 'triggered') &&
												'border-green-500/40 bg-green-500/10 text-green-600 dark:text-green-400',
											result.status === 'updating' && 'border-primary/40 bg-primary/10 text-primary',
											result.status === 'skipped_offline' &&
												'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
											result.status === 'failed' && 'border-destructive/40 bg-destructive/10 text-destructive',
											result.status === 'pending' && 'border-border bg-muted/40 text-muted-foreground/60'
										)}
									>
										{#if result.status === 'updated' || result.status === 'triggered'}
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
										<span class="block truncate font-medium">{result.environmentName}</span>
										{#if result.status === 'updating'}
											<div class="mt-1.5 h-1 overflow-hidden rounded-full bg-muted">
												<div class="update-all-capbar h-full rounded-full bg-primary"></div>
											</div>
										{:else if result.error}
											<span class="block truncate text-xs text-muted-foreground" title={result.error}>{result.error}</span>
										{/if}
									</div>

									<span
										class={cn(
											'shrink-0 text-xs',
											result.status === 'updating' ? 'text-primary' : 'text-muted-foreground',
											result.status === 'skipped_offline' && 'text-amber-600 dark:text-amber-400',
											result.status === 'failed' && 'text-destructive'
										)}
									>
										{statusLabel(result.status)}
									</span>
								</li>
							{/each}
						</ul>
					</ScrollArea.Root>
				{:else}
					<div class="flex items-center gap-2 py-2 text-sm text-muted-foreground">
						<Spinner class="size-4" />
						<span>{m.environments_update_all_in_progress()}</span>
					</div>
				{/if}
			</div>
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

<style>
	/* Indeterminate, capped progress: a short segment slides across the track so a
	   busy environment never reads as "100% complete" while the upgrade is still
	   running (we have no real per-step percentage from the backend). */
	@keyframes update-all-indeterminate {
		0% {
			transform: translateX(-100%);
		}
		100% {
			transform: translateX(250%);
		}
	}
	.update-all-capbar {
		width: 40%;
		animation: update-all-indeterminate 1.4s ease-in-out infinite;
	}
</style>
