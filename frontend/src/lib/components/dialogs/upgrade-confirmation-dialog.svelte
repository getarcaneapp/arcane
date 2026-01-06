<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import Spinner from '$lib/components/ui/spinner/spinner.svelte';
	import * as m from '$lib/paraglide/messages';
	import { onDestroy } from 'svelte';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import { cn } from '$lib/utils';
	import { AlertIcon, InfoIcon, SuccessIcon } from '$lib/icons';
	import type { AppVersionInformation } from '$lib/types/application-configuration';

	let {
		open = $bindable(false),
		version,
		expectedVersion,
		expectedDigest,
		onConfirm,
		environmentName,
		environmentId,
		upgrading = $bindable(false)
	}: {
		open?: boolean;
		version: string;
		expectedVersion?: string;
		expectedDigest?: string;
		onConfirm: () => void | Promise<void>;
		environmentName?: string;
		environmentId?: string;
		upgrading?: boolean;
	} = $props();

	const isRemoteEnvironment = $derived(!!environmentName);
	const targetDescription = $derived(isRemoteEnvironment ? `remote environment "${environmentName}"` : m.upgrade_this_system());

	let upgradeStatus = $state<'upgrading' | 'waiting' | 'ready' | 'countdown'>('upgrading');
	let countdown = $state(10);
	let pollAbort = $state<{ aborted: boolean } | null>(null);
	let countdownInterval: ReturnType<typeof setInterval> | null = null;
	let fallbackTimeout: ReturnType<typeof setTimeout> | null = null;
	let baselineVersionInfo = $state<AppVersionInformation | null>(null);
	let lastSeenVersionInfo = $state<AppVersionInformation | null>(null);
	let consecutiveHealthyChecks = $state(0);

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
			expectedVersion,
			expectedDigest: short(expectedDigest)
		});

		pollAbort = { aborted: false };
		const abortRef = pollAbort;

		upgradeStatus = 'waiting';
		consecutiveHealthyChecks = 0;

		const startedAt = Date.now();
		const timeoutMs = 3 * 60 * 1000;
		let delayMs = 1000;

		while (!abortRef.aborted && Date.now() - startedAt < timeoutMs) {
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

			try {
				const info = await systemUpgradeService.getVersionInfo(envId);
				lastSeenVersionInfo = info;

				const expVer = expectedVersion?.trim();
				const expDig = expectedDigest?.trim();
				const ok = matchesExpected(info);
				const changed = versionInfoChanged(baselineVersionInfo, info);

				log('version-check', {
					currentVersion: info.currentVersion,
					currentDigest: short(info.currentDigest),
					revision: short(info.revision, 8),
					baselineVersion: baselineVersionInfo?.currentVersion,
					baselineDigest: short(baselineVersionInfo?.currentDigest),
					ok
				});

				const verified = expVer || expDig ? ok : !!baselineVersionInfo && changed;
				if (verified) {
					log('verified', {
						mode: expVer || expDig ? 'expected' : 'baseline-change',
						currentVersion: info.currentVersion,
						currentDigest: short(info.currentDigest)
					});
					upgradeStatus = 'ready';
					setTimeout(() => startCountdown(), 1500);
					return;
				}
			} catch (err) {
				log('version-endpoint-error', err);
			}

			await new Promise((r) => setTimeout(r, 2000));
		}

		if (!abortRef.aborted) {
			log('monitor-timeout', { timeoutMs });
			upgradeStatus = 'upgrading';
			upgrading = false;
		}
	}

	async function captureBaseline() {
		try {
			baselineVersionInfo = await systemUpgradeService.getVersionInfo(environmentId ?? '0');
			lastSeenVersionInfo = baselineVersionInfo;
			log('baseline', {
				currentVersion: baselineVersionInfo.currentVersion,
				currentDigest: short(baselineVersionInfo.currentDigest),

				revision: short(baselineVersionInfo.revision, 8)
			});
		} catch (err) {
			log('baseline-error', err);
			baselineVersionInfo = null;
		}
	}

	function startCountdown() {
		upgradeStatus = 'countdown';
		countdown = 10;
		countdownInterval = setInterval(() => {
			countdown--;
			if (countdown <= 0) {
				if (countdownInterval) clearInterval(countdownInterval);
				reloadPage();
			}
		}, 1000);
	}

	function reloadPage() {
		window.location.reload();
	}

	async function handleConfirm() {
		upgrading = true;
		upgradeStatus = 'upgrading';
		log('confirm', {
			isRemoteEnvironment,
			environmentId: environmentId ?? '0',
			expectedVersion,
			expectedDigest: short(expectedDigest)
		});

		if (!isRemoteEnvironment) {
			await captureBaseline();
		}
		try {
			await onConfirm();
		} catch (err) {
			log('trigger-error', err);
			upgrading = false;
			return;
		}
		if (!upgrading) return;

		if (!isRemoteEnvironment) {
			if (fallbackTimeout) clearTimeout(fallbackTimeout);
			fallbackTimeout = setTimeout(
				() => {
					log('fallback-reload', { reason: 'timeout' });
					if (upgradeStatus !== 'countdown') {
						reloadPage();
					}
				},
				4 * 60 * 1000
			);

			monitorUpgrade();
		}
	}

	onDestroy(() => {
		log('destroy');
		if (countdownInterval) clearInterval(countdownInterval);
		if (fallbackTimeout) clearTimeout(fallbackTimeout);
		if (pollAbort) pollAbort.aborted = true;
	});
</script>

<Dialog.Root bind:open>
	<Dialog.Content
		class={cn('sm:max-w-[500px]', upgrading && '[&>button]:hidden')}
		onInteractOutside={(e: Event) => {
			if (upgrading) e.preventDefault();
		}}
	>
		<Dialog.Header>
			<Dialog.Title>
				{#if upgrading}
					{m.upgrade_in_progress()}
				{:else}
					{m.upgrade_confirm_title()}
				{/if}
			</Dialog.Title>
			{#if !upgrading}
				<Dialog.Description>
					{#if isRemoteEnvironment}
						{m.upgrade_remote_description({ targetDescription, version })}
					{:else}
						{m.upgrade_confirm_description({ version })}
					{/if}
				</Dialog.Description>
			{/if}
		</Dialog.Header>

		{#if upgrading}
			<div class="space-y-4 py-4">
				<div class="flex items-center justify-center gap-2 text-sm">
					{#if upgradeStatus === 'countdown'}
						<SuccessIcon class="size-5 text-green-500" />
						<span class="font-medium text-green-600 dark:text-green-400">{m.upgrade_status_complete()}</span>
					{:else if upgradeStatus === 'ready'}
						<SuccessIcon class="size-5 animate-pulse text-green-500" />
						<span class="font-medium text-green-600 dark:text-green-400">{m.upgrade_status_detected()}</span>
					{:else}
						<Spinner class="text-primary size-5" />
						<span class="font-medium">
							{#if upgradeStatus === 'upgrading'}
								{m.upgrade_status_pulling()}
							{:else if upgradeStatus === 'waiting'}
								{m.upgrade_status_checking()}
							{/if}
						</span>
					{/if}
				</div>

				{#if upgradeStatus === 'countdown'}
					<div class="rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800 dark:bg-green-950/20">
						<p class="flex items-center gap-2 text-sm font-medium text-green-800 dark:text-green-200">
							<InfoIcon class="size-4" />
							{m.upgrade_reload_auto({ countdown })}
						</p>
					</div>
					<div class="flex justify-center">
						<ArcaneButton
							action="base"
							onclick={reloadPage}
							size="sm"
							customLabel={m.upgrade_reload_now()}
							class="w-full sm:w-auto"
						/>
					</div>
				{:else if upgradeStatus === 'waiting'}
					<div class="rounded-lg border border-blue-200 bg-blue-50 p-3 dark:border-blue-800 dark:bg-blue-950/20">
						<p class="flex items-center gap-2 text-sm font-medium text-blue-800 dark:text-blue-200">
							<InfoIcon class="size-4" />
							{m.upgrade_wait_info()}
						</p>
					</div>
				{:else}
					<div class="rounded-lg border border-blue-200 bg-blue-50 p-3 dark:border-blue-800 dark:bg-blue-950/20">
						<p class="flex items-center gap-2 text-sm font-medium text-blue-800 dark:text-blue-200">
							<InfoIcon class="size-4" />
							{m.upgrade_wait_message()}
						</p>
					</div>
				{/if}
			</div>
		{:else}
			<div class="space-y-3 py-4">
				<p class="text-sm font-medium">{m.upgrade_confirm_what_happens()}</p>
				<ul class="text-muted-foreground list-inside list-disc space-y-1 text-sm">
					<li>{m.upgrade_step_pull()}</li>
					<li>{m.upgrade_step_stop()}</li>
					<li>{m.upgrade_step_start()}</li>
					<li>{m.upgrade_step_preserve()}</li>
				</ul>

				<div class="rounded-lg border border-orange-200 bg-orange-50 p-3 dark:border-orange-800 dark:bg-orange-950/20">
					<p class="flex items-center gap-2 text-sm font-medium text-orange-800 dark:text-orange-200">
						<AlertIcon class="size-4" />
						{m.upgrade_warning_interruption()}
					</p>
				</div>
			</div>

			<Dialog.Footer>
				<ArcaneButton action="cancel" onclick={() => (open = false)} />
				<ArcaneButton action="update" customLabel={m.upgrade_now()} onclick={handleConfirm} />
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
