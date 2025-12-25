<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import Spinner from '$lib/components/ui/spinner/spinner.svelte';
	import * as m from '$lib/paraglide/messages';
	import { onDestroy } from 'svelte';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import { cn } from '$lib/utils';
	import { AlertIcon, InfoIcon, SuccessIcon } from '$lib/icons';

	let {
		open = $bindable(false),
		version,
		onConfirm,
		environmentName,
		environmentId,
		upgrading = $bindable(false)
	}: {
		open?: boolean;
		version: string;
		onConfirm: () => void;
		environmentName?: string;
		environmentId?: string;
		upgrading?: boolean;
	} = $props();

	const isRemoteEnvironment = $derived(!!environmentName);
	const targetDescription = $derived(isRemoteEnvironment ? `remote environment "${environmentName}"` : m.upgrade_this_system());

	let upgradeStatus = $state<'upgrading' | 'waiting' | 'ready' | 'countdown'>('upgrading');
	let countdown = $state(10);
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let countdownInterval: ReturnType<typeof setInterval> | null = null;
	let fallbackTimeout: ReturnType<typeof setTimeout> | null = null;
	let consecutiveSuccessfulChecks = $state(0);

	async function startHealthPolling() {
		console.log('[Upgrade] Starting upgrade monitoring...');

		// Wait 15 seconds for the upgrade to start, then begin polling
		setTimeout(() => {
			upgradeStatus = 'waiting';
			consecutiveSuccessfulChecks = 0;
			console.log('[Upgrade] Starting health polling...');

			pollInterval = setInterval(async () => {
				const { healthy } = await systemUpgradeService.checkHealth(environmentId);

				console.log('[Upgrade] Health check:', { healthy, consecutiveSuccessfulChecks, environmentId });

				if (healthy) {
					consecutiveSuccessfulChecks++;
					console.log('[Upgrade] Container is up! Consecutive checks:', consecutiveSuccessfulChecks);

					if (consecutiveSuccessfulChecks >= 3) {
						console.log('[Upgrade] 3 consecutive checks passed! Starting countdown...');
						if (pollInterval) clearInterval(pollInterval);
						if (fallbackTimeout) clearTimeout(fallbackTimeout);
						upgradeStatus = 'ready';
						setTimeout(() => {
							startCountdown();
						}, 2000);
					}
				} else {
					consecutiveSuccessfulChecks = 0;
					console.log('[Upgrade] Container not ready yet, waiting...');
				}
			}, 2000);

			// Stop polling after 3 minutes and show error
			setTimeout(() => {
				if (pollInterval && upgradeStatus !== 'ready') {
					console.log('[Upgrade] 3-minute timeout reached, stopping polling');
					clearInterval(pollInterval);
					pollInterval = null;
					upgradeStatus = 'upgrading';
					upgrading = false;
				}
			}, 180000);

			// Force reload after 2.5 minutes as fallback
			fallbackTimeout = setTimeout(() => {
				if (upgradeStatus !== 'countdown') {
					console.log('[Upgrade] Fallback timeout reached, forcing reload');
					if (pollInterval) clearInterval(pollInterval);
					reloadPage();
				}
			}, 150000);
		}, 15000);
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

	function handleConfirm() {
		upgrading = true;
		upgradeStatus = 'upgrading';
		onConfirm();
		if (!isRemoteEnvironment) {
			startHealthPolling();
		}
	}

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
		if (countdownInterval) clearInterval(countdownInterval);
		if (fallbackTimeout) clearTimeout(fallbackTimeout);
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
