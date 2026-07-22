<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as ScrollArea from '$lib/components/ui/scroll-area';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import Spinner from '$lib/components/ui/spinner/spinner.svelte';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import { onDestroy } from 'svelte';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import BaseAPIService from '$lib/services/api-service';
	import { cn } from '$lib/utils';
	import { ExternalLinkIcon, SuccessIcon } from '$lib/icons';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { formatDistanceToNow } from 'date-fns';
	import ReleaseNotes from '$lib/components/release-notes.svelte';
	import VersionUpdateSummary from './version-update-summary.svelte';

	// open/upgrading have no $bindable fallback: upstream binds can start out
	// undefined, and binding undefined to a $bindable with a fallback throws
	// props_invalid_value. Undefined reads as falsy.
	let {
		open = $bindable(undefined),
		versionInformation,
		onConfirm,
		environmentName,
		environmentId,
		canInstall = true,
		debug = false,
		upgrading = $bindable(undefined)
	}: {
		open?: boolean;
		versionInformation?: AppVersionInformation;
		onConfirm: () => void | Promise<void>;
		environmentName?: string;
		environmentId?: string;
		canInstall?: boolean;
		debug?: boolean;
		upgrading?: boolean;
	} = $props();
	const queryClient = useQueryClient();

	const isRemoteEnvironment = $derived(!!environmentName);
	const expectedVersion = $derived(versionInformation?.newestVersion);
	const expectedDigest = $derived(versionInformation?.newestDigest);
	const isSemver = $derived(!!versionInformation?.isSemverVersion);

	const debugReleaseEnabled = $derived(open && debug && !versionInformation?.releaseNotes);
	const debugReleaseQuery = createQuery(() => ({
		queryKey: ['github', 'getarcaneapp', 'arcane', 'latest-release'],
		queryFn: async () => {
			const res = await fetch('https://api.github.com/repos/getarcaneapp/arcane/releases/latest', {
				headers: { Accept: 'application/vnd.github+json' }
			});
			if (!res.ok) throw new Error(`GitHub returned ${res.status}`);
			const data = await res.json();
			return {
				tag: (data.tag_name as string | undefined) ?? '',
				body: (data.body as string | undefined) ?? '',
				publishedAt: (data.published_at as string | undefined) ?? '',
				url: (data.html_url as string | undefined) ?? ''
			};
		},
		enabled: debugReleaseEnabled,
		staleTime: 5 * 60 * 1000,
		retry: false
	}));

	const debugRelease = $derived(debugReleaseQuery.data ?? null);
	const debugFetching = $derived(debugReleaseQuery.isFetching);
	const debugFetchSettled = $derived(debugReleaseQuery.isSuccess || debugReleaseQuery.isError);

	const trackingTag = $derived(versionInformation?.currentTag ?? '');
	const newestVersion = $derived(versionInformation?.newestVersion || debugRelease?.tag || '');

	const installLabel = $derived.by(() => {
		if (isSemver && versionInformation?.newestVersion) {
			return m.upgrade_to_version({ version: versionInformation.newestVersion });
		}
		if (trackingTag) {
			return m.upgrade_update_tag({ tag: trackingTag });
		}
		return m.update_center_install();
	});

	const installingTitle = $derived.by(() => {
		if (versionInformation?.newestVersion) return m.update_center_installing({ version: versionInformation.newestVersion });
		if (trackingTag) return m.upgrade_update_tag({ tag: trackingTag });
		return m.upgrade_in_progress();
	});

	const effectiveReleaseNotes = $derived(versionInformation?.releaseNotes?.trim() || debugRelease?.body?.trim() || '');
	const effectiveReleasedAt = $derived(versionInformation?.releasedAt || debugRelease?.publishedAt || '');
	const effectiveReleaseUrl = $derived(versionInformation?.releaseUrl || debugRelease?.url || '');

	const releasedAgo = $derived.by(() => {
		const at = effectiveReleasedAt;
		if (!at) return '';
		try {
			const date = new Date(at);
			if (Number.isNaN(date.getTime())) return '';
			return formatDistanceToNow(date, { addSuffix: true });
		} catch {
			return '';
		}
	});

	let upgradeStatus = $state<'upgrading' | 'waiting' | 'ready' | 'complete'>('upgrading');
	let pollAbort = $state<{ aborted: boolean } | null>(null);
	let fallbackTimeout: ReturnType<typeof setTimeout> | null = null;
	let baselineVersionInfo = $state<AppVersionInformation | null>(null);
	let consecutiveHealthyChecks = $state(0);
	// Wall-clock millis at which the trigger API call resolved successfully.
	// `null` means it hasn't returned yet (or errored). Used by the polling loop
	// to decide whether enough time has passed since the agent was told to
	// upgrade for "no version change" to be conclusive.
	let triggerResolvedAt = $state<number | null>(null);

	function short(v?: string | null, n = 12): string {
		if (!v) return '';
		const s = String(v);
		return s.length > n ? s.slice(0, n) : s;
	}

	function log(step: string, data?: unknown) {
		if (data === undefined) {
			console.log(`[Upgrade] ${step}`);
			return;
		}
		console.log(`[Upgrade] ${step}`, data);
	}

	function versionInfoChanged(a: AppVersionInformation | null, b: AppVersionInformation | null) {
		if (!a || !b) return false;
		return (
			(a.currentDigest && b.currentDigest && a.currentDigest !== b.currentDigest) ||
			a.currentVersion !== b.currentVersion ||
			a.revision !== b.revision ||
			a.displayVersion !== b.displayVersion
		);
	}

	function matchesExpected(info: AppVersionInformation) {
		const expVer = expectedVersion?.trim();
		const expDig = expectedDigest?.trim();
		if (expVer) return info.currentVersion === expVer;
		if (expDig) return info.currentDigest === expDig;
		return true;
	}

	async function monitorUpgrade() {
		const envId = environmentId ?? '0';
		log('monitor-start', {
			envId,
			isRemoteEnvironment,
			expectedVersion,
			expectedDigest: short(expectedDigest),
			baselineVersion: baselineVersionInfo?.currentVersion,
			baselineDigest: short(baselineVersionInfo?.currentDigest)
		});

		pollAbort = { aborted: false };
		const abortRef = pollAbort;

		upgradeStatus = 'waiting';
		consecutiveHealthyChecks = 0;

		const startedAt = Date.now();
		const timeoutMs = 3 * 60 * 1000;
		let delayMs = 1000;

		// After the trigger API reports back, the agent has had its chance to act.
		// If the running version still equals what it was BEFORE we triggered, the
		// upgrade was a no-op (already on latest, or nothing to do) and we can
		// finish immediately. NO_OP_GRACE_MS is how long we give the agent to begin
		// a restart after the trigger returns before we declare no-op.
		const NO_OP_GRACE_MS = 5000;

		// If we've successfully fetched the version this many times AND the trigger
		// promise hasn't resolved, the agent is clearly reachable but our POST got
		// lost (e.g. its response was dropped during a no-op or an internal restart).
		// Treat the trigger as having implicitly resolved so the no-op detector can
		// run. This is a fact-based fallback, not a wall-clock guess: it's gated on
		// the agent demonstrably answering us via a different request.
		let successfulPolls = 0;

		while (!abortRef.aborted && Date.now() - startedAt < timeoutMs) {
			// For LOCAL upgrades, the /health endpoint reflects whether *this* Arcane
			// is back up. For REMOTE upgrades, that endpoint just pings docker — it
			// does not tell us whether the remote agent has restarted. Skip the
			// health gate for remote and rely on /environments/{id}/version
			// reachability + a digest/version diff as the signal instead.
			if (!isRemoteEnvironment) {
				const { healthy } = await systemUpgradeService.checkHealth(envId);
				if (!healthy) {
					log('health', { healthy, consecutiveHealthyChecks, backoffMs: delayMs });
					consecutiveHealthyChecks = 0;
					await new Promise((r) => setTimeout(r, delayMs));
					delayMs = Math.min(Math.round(delayMs * 1.4), 5000);
					continue;
				}

				consecutiveHealthyChecks++;
				log('health', { healthy, consecutiveHealthyChecks });
				if (consecutiveHealthyChecks < 2) {
					await new Promise((r) => setTimeout(r, 1000));
					continue;
				}
			}

			try {
				const info = await systemUpgradeService.getVersionInfo(envId);
				successfulPolls += 1;

				// Promote a hung trigger to "resolved" once the agent has answered
				// at least 2 unrelated requests successfully. The agent is alive and
				// has had time to act on our POST — its response just never came back.
				if (triggerResolvedAt === null && successfulPolls >= 2) {
					triggerResolvedAt = Date.now();
					log('trigger-implicit-resolved', {
						reason: 'agent-answered-poll-without-trigger-response',
						successfulPolls
					});
				}

				const expVer = expectedVersion?.trim();
				const expDig = expectedDigest?.trim();
				const ok = matchesExpected(info);
				const changed = versionInfoChanged(baselineVersionInfo, info);

				const triggerSettledAt = triggerResolvedAt;
				const sinceTriggerMs = triggerSettledAt === null ? null : Date.now() - triggerSettledAt;

				log('version-check', {
					currentVersion: info.currentVersion,
					currentDigest: short(info.currentDigest),
					revision: short(info.revision, 8),
					baselineVersion: baselineVersionInfo?.currentVersion,
					baselineDigest: short(baselineVersionInfo?.currentDigest),
					expVer,
					expDig: short(expDig),
					ok,
					changed,
					sinceTriggerMs,
					successfulPolls
				});

				const verified = expVer || expDig ? ok : !!baselineVersionInfo && changed;
				if (verified) {
					log('verified', {
						mode: expVer || expDig ? 'expected' : 'baseline-change',
						currentVersion: info.currentVersion,
						currentDigest: short(info.currentDigest)
					});
					upgradeStatus = 'ready';
					if (isRemoteEnvironment) {
						setTimeout(() => (upgradeStatus = 'complete'), 1500);
					} else {
						reloadPage();
					}
					return;
				}

				// no-op detection: trigger API has returned successfully,
				// the agent has had NO_OP_GRACE_MS to begin a restart, and the version
				// has not moved from baseline. Conclusion: there was nothing to do.
				if (
					!expVer &&
					!expDig &&
					!!baselineVersionInfo &&
					!changed &&
					sinceTriggerMs !== null &&
					sinceTriggerMs >= NO_OP_GRACE_MS
				) {
					log('no-op-detected', {
						sinceTriggerMs,
						currentDigest: short(info.currentDigest),
						baselineDigest: short(baselineVersionInfo?.currentDigest)
					});
					upgradeStatus = 'ready';
					setTimeout(() => {
						if (isRemoteEnvironment) {
							upgradeStatus = 'complete';
						} else {
							closeAfterComplete();
						}
					}, 600);
					return;
				}
			} catch (err) {
				log('version-endpoint-error', err);
			}

			await new Promise((r) => setTimeout(r, 2000));
		}

		if (!abortRef.aborted) {
			log('monitor-timeout', { timeoutMs, isRemoteEnvironment });
			if (isRemoteEnvironment) {
				upgradeStatus = 'complete';
			} else {
				if (fallbackTimeout) {
					clearTimeout(fallbackTimeout);
					fallbackTimeout = null;
				}
				upgradeStatus = 'upgrading';
				upgrading = false;
				BaseAPIService.setUpgradeInProgress(false);
			}
		}
	}

	async function captureBaseline() {
		try {
			baselineVersionInfo = await queryClient.fetchQuery({
				queryKey: queryKeys.system.versionInfo(environmentId ?? '0'),
				queryFn: () => systemUpgradeService.getVersionInfo(environmentId ?? '0'),
				staleTime: 0
			});
			if (!baselineVersionInfo) return;

			const baseline = baselineVersionInfo;
			log('baseline', {
				currentVersion: baseline.currentVersion,
				currentDigest: short(baseline.currentDigest),
				revision: short(baseline.revision, 8)
			});
		} catch (err) {
			log('baseline-error', err);
			baselineVersionInfo = null;
		}
	}

	function reloadPage() {
		window.location.reload();
	}

	async function handleConfirm() {
		upgrading = true;
		upgradeStatus = 'upgrading';
		// A local upgrade restarts THIS manager; flag it so the restart's version-
		// mismatch 401s are treated as a recoverable reconnect, not a logout.
		if (!isRemoteEnvironment) BaseAPIService.setUpgradeInProgress(true);
		log('confirm', {
			isRemoteEnvironment,
			environmentId: environmentId ?? '0',
			expectedVersion,
			expectedDigest: short(expectedDigest)
		});

		await captureBaseline();
		log('baseline-done', { hasBaseline: !!baselineVersionInfo });

		// The trigger POST can hang indefinitely (apiClient has no timeout) when the
		// agent restarts mid-request. Don't block monitor startup on its completion —
		// race it against a short delay so we can detect synchronous failures (auth,
		// validation) but proceed to polling for any longer-running call. When the
		// trigger eventually returns successfully we record the timestamp so the
		// poll loop can run its no-op detection against it.
		triggerResolvedAt = null;
		let triggerErrored = false;
		const triggerPromise = Promise.resolve()
			.then(() => onConfirm())
			.then(() => {
				triggerResolvedAt = Date.now();
				log('trigger-resolved');
			})
			.catch((err) => {
				log('trigger-error', err);
				triggerErrored = true;
			});

		await Promise.race([triggerPromise, new Promise((r) => setTimeout(r, 1500))]);

		if (triggerErrored) {
			upgrading = false;
			BaseAPIService.setUpgradeInProgress(false);
			return;
		}
		if (!upgrading) {
			log('aborted-after-onConfirm', { upgrading });
			return;
		}

		if (fallbackTimeout) clearTimeout(fallbackTimeout);
		fallbackTimeout = setTimeout(
			() => {
				log('fallback-timeout', { reason: 'timeout', isRemoteEnvironment });
				if (isRemoteEnvironment) {
					if (upgradeStatus !== 'complete') upgradeStatus = 'complete';
				} else {
					reloadPage();
				}
			},
			4 * 60 * 1000
		);

		log('starting-monitor', { isRemoteEnvironment, environmentId });
		monitorUpgrade();

		// If the trigger eventually errors after monitoring started, surface it but
		// don't yank the dialog — monitor will time out on its own.
		void triggerPromise.then(() => {
			if (triggerErrored) log('trigger-errored-after-monitor-start');
		});
	}

	function closeAfterComplete() {
		upgrading = false;
		BaseAPIService.setUpgradeInProgress(false);
		open = false;
	}

	type StepState = 'done' | 'active' | 'pending';
	const steps = $derived.by<Array<{ key: string; label: string; state: StepState }>>(() => {
		const labels = isRemoteEnvironment
			? [
					{ key: 'pull', label: m.update_center_step_pull() },
					{ key: 'restart', label: m.update_center_step_restart() },
					{ key: 'verify', label: m.update_center_step_verify() }
				]
			: [
					{ key: 'pull', label: m.update_center_step_pull() },
					{ key: 'restart', label: m.update_center_step_restart() },
					{ key: 'verify', label: m.update_center_step_verify() },
					{ key: 'reload', label: m.update_center_step_reload() }
				];

		const phaseIndex = (() => {
			switch (upgradeStatus) {
				case 'upgrading':
					return 0;
				case 'waiting':
					return 1;
				case 'ready':
					return 2;
				case 'complete':
					return labels.length;
				default:
					return 0;
			}
		})();

		return labels.map(({ key, label }, idx) => ({
			key,
			label,
			state: idx < phaseIndex ? 'done' : idx === phaseIndex ? 'active' : 'pending'
		}));
	});

	onDestroy(() => {
		log('destroy');
		if (fallbackTimeout) clearTimeout(fallbackTimeout);
		if (pollAbort) pollAbort.aborted = true;
		BaseAPIService.setUpgradeInProgress(false);
	});
</script>

<Dialog.Root {open} onOpenChange={(nextOpen) => (open = nextOpen)}>
	<Dialog.Content
		class={cn('gap-0 overflow-hidden p-0 sm:max-w-[560px]', upgrading && '[&>button]:hidden')}
		onInteractOutside={(e: Event) => {
			if (upgrading) e.preventDefault();
		}}
	>
		{#if upgrading}
			<div class="px-6 pt-6 pb-2">
				<Dialog.Header>
					<Dialog.Title class="text-lg">{installingTitle}</Dialog.Title>
				</Dialog.Header>
			</div>

			<div class="px-6 pb-6">
				<ol class="space-y-3 py-2">
					{#each steps as step (step.key)}
						<li class="flex items-center gap-3 text-sm">
							<span
								class={cn(
									'flex size-6 shrink-0 items-center justify-center rounded-full border transition-colors',
									step.state === 'done' && 'border-green-500/40 bg-green-500/10 text-green-600 dark:text-green-400',
									step.state === 'active' && 'border-primary/40 bg-primary/10 text-primary',
									step.state === 'pending' && 'border-border bg-muted/40 text-muted-foreground/60'
								)}
							>
								{#if step.state === 'done'}
									<SuccessIcon class="size-3.5" />
								{:else if step.state === 'active'}
									<Spinner class="size-3.5" />
								{:else}
									<span class="size-1.5 rounded-full bg-current opacity-60"></span>
								{/if}
							</span>
							<span
								class={cn(
									'transition-colors',
									step.state === 'done' && 'text-foreground',
									step.state === 'active' && 'font-medium text-foreground',
									step.state === 'pending' && 'text-muted-foreground'
								)}
							>
								{step.label}
							</span>
						</li>
					{/each}
				</ol>

				{#if upgradeStatus === 'complete'}
					<div class="mt-4 flex items-center justify-between rounded-lg border border-green-500/20 bg-green-500/5 px-3 py-2.5">
						<p class="flex items-center gap-2 text-sm font-medium text-green-700 dark:text-green-400">
							<SuccessIcon class="size-4" />
							{m.update_center_complete()}
						</p>
						<ArcaneButton action="base" onclick={closeAfterComplete} size="sm" customLabel={m.common_close()} />
					</div>
				{:else}
					<p class="mt-4 text-xs text-muted-foreground">
						{m.update_center_estimated()}
					</p>
				{/if}
			</div>
		{:else}
			<div class="px-6 pt-6 pb-4">
				<Dialog.Header class="space-y-3">
					<Dialog.Title class="text-lg">
						{#if isRemoteEnvironment && versionInformation?.newestVersion}
							{m.upgrade_remote_description({
								targetDescription: environmentName ?? '',
								version: versionInformation.newestVersion
							})}
						{:else if isRemoteEnvironment}
							{m.update_center_remote_title({ target: environmentName ?? '' })}
						{:else if !isSemver && trackingTag}
							{m.upgrade_update_tag({ tag: trackingTag })}
						{:else}
							{m.update_center_title()}
						{/if}
					</Dialog.Title>

					<VersionUpdateSummary {versionInformation} {newestVersion} {releasedAgo} />
				</Dialog.Header>
			</div>

			<div class="border-t border-border/60">
				<div class="flex items-center justify-between px-6 pt-4 pb-2">
					<h3 class="text-sm font-semibold text-foreground">{m.update_center_whats_new()}</h3>
					{#if effectiveReleaseUrl}
						<a
							href={effectiveReleaseUrl}
							target="_blank"
							rel="noopener noreferrer"
							class="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
						>
							{m.update_center_view_full_release()}
							<ExternalLinkIcon class="size-3" />
						</a>
					{/if}
				</div>

				<ScrollArea.Root class="h-[260px] px-6 pb-4">
					{#if effectiveReleaseNotes}
						<ReleaseNotes markdown={effectiveReleaseNotes} />
					{:else if debug && debugFetching}
						<div class="flex items-center gap-2 text-sm text-muted-foreground">
							<Spinner class="size-3.5" />
							<span>Loading release notes…</span>
						</div>
					{:else if debug && debugFetchSettled && !effectiveReleaseNotes}
						<p class="text-sm text-muted-foreground italic">
							{m.update_center_release_notes_unavailable()}
						</p>
					{:else}
						<p class="text-sm text-muted-foreground italic">
							{m.update_center_release_notes_unavailable()}
						</p>
					{/if}
				</ScrollArea.Root>
			</div>

			<div class="border-t border-border/60 bg-muted/30 px-6 py-3">
				<p class="text-xs leading-relaxed text-muted-foreground">
					{m.update_center_summary()}
				</p>
			</div>

			<Dialog.Footer class="border-t border-border/60 px-6 py-4">
				<ArcaneButton action="cancel" customLabel={m.update_center_later()} onclick={() => (open = false)} />
				{#if canInstall}
					<ArcaneButton action="update" customLabel={installLabel} onclick={handleConfirm} />
				{/if}
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
