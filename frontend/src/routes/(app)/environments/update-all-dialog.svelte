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
	import { SuccessIcon, CheckIcon, ClockIcon, AlertIcon, AlertTriangleIcon } from '$lib/icons';

	// open has no $bindable fallback: upstream binds can start out undefined, and
	// binding undefined to a $bindable with a fallback throws props_invalid_value.
	let { open = $bindable(undefined), onFinished }: { open?: boolean; onFinished?: () => void | Promise<void> } = $props();

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
		phase = 'confirm';
		job = null;
		reconnecting = false;
	}

	function schedulePoll() {
		if (!pollActive) return;
		pollTimer = setTimeout(poll, POLL_INTERVAL_MS);
	}

	async function poll() {
		if (!pollActive) return;
		try {
			const next = await systemUpgradeService.getUpdateAllStatus();
			reconnecting = false;
			job = next;
			if (next.status === 'completed' || next.status === 'failed') {
				stopPolling();
				phase = 'finished';
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
		try {
			job = await systemUpgradeService.triggerUpdateAll();
		} catch {
			toast.error(m.environments_update_all_trigger_failed());
			resetState();
			return;
		}

		if (phase !== 'running') return;

		if (job && (job.status === 'completed' || job.status === 'failed')) {
			phase = 'finished';
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

	onDestroy(stopPolling);

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
				return m.environments_update_all_status_updating();
			case 'updated':
				return m.environments_update_all_status_updated();
			case 'triggered':
				return m.environments_update_all_status_triggered();
			case 'skipped_up_to_date':
				return m.environments_update_all_status_skipped_up_to_date();
			case 'skipped_offline':
				return m.environments_update_all_status_skipped_offline();
			case 'failed':
				return m.environments_update_all_status_failed();
			default:
				return m.environments_update_all_status_pending();
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

		{#if phase !== 'confirm'}
			<div class="space-y-3">
				{#if reconnecting}
					<div class="text-muted-foreground flex items-center gap-2 text-sm">
						<Spinner class="size-4" />
						<span>{m.environments_update_all_manager_restarting()}</span>
					</div>
				{/if}

				{#if job?.results?.length}
					{#if phase === 'running'}
						<div class="space-y-1.5">
							<div class="text-muted-foreground flex items-center justify-end text-xs">
								<span>{m.environments_update_all_progress({ done: doneCount, total: totalCount })}</span>
							</div>
							<div class="bg-muted h-1.5 overflow-hidden rounded-full">
								<div class="bg-primary h-full rounded-full transition-all duration-500" style="width: {progressPct}%"></div>
							</div>
						</div>
					{/if}

					<ScrollArea.Root class="max-h-72">
						<ul class="divide-border divide-y">
							{#each job.results as result (result.environmentId)}
								<li class="flex items-center gap-3 py-2.5 text-sm">
									<span
										class={cn(
											'flex size-7 shrink-0 items-center justify-center rounded-full border',
											(result.status === 'updated' || result.status === 'triggered') &&
												'border-green-500/40 bg-green-500/10 text-green-600 dark:text-green-400',
											result.status === 'updating' && 'border-primary/40 bg-primary/10 text-primary',
											result.status === 'skipped_up_to_date' && 'border-border bg-muted/40 text-muted-foreground',
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
										{:else if result.status === 'skipped_up_to_date'}
											<CheckIcon class="size-3.5" />
										{:else if result.status === 'skipped_offline'}
											<AlertIcon class="size-3.5" />
										{:else if result.status === 'failed'}
											<AlertTriangleIcon class="size-3.5" />
										{:else}
											<ClockIcon class="size-3.5" />
										{/if}
									</span>

									<div class="flex min-w-0 flex-1 flex-col">
										<span class="truncate font-medium">{result.environmentName}</span>
										{#if result.status === 'updating'}
											<div class="bg-muted mt-1.5 h-1 overflow-hidden rounded-full">
												<div class="update-all-capbar bg-primary h-full rounded-full"></div>
											</div>
										{:else if result.error}
											<span class="text-muted-foreground truncate text-xs">{result.error}</span>
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
					<div class="text-muted-foreground flex items-center gap-2 py-2 text-sm">
						<Spinner class="size-4" />
						<span>{m.environments_update_all_in_progress()}</span>
					</div>
				{/if}
			</div>
		{/if}

		<Dialog.Footer>
			{#if phase === 'confirm'}
				<Button variant="outline" onclick={() => (open = false)}>{m.common_cancel()}</Button>
				<Button onclick={handleConfirm}>{m.environments_update_all_confirm()}</Button>
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
