<script lang="ts">
	import { onDestroy } from 'svelte';
	import QRCode from 'qrcode';
	import { toast } from 'svelte-sonner';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { CloseIcon, RefreshIcon, SmartphoneIcon, AlertTriangleIcon, SuccessIcon } from '$lib/icons';
	import { deviceService } from '$lib/services/device-service';
	import type { PairingCodeResponse, PairingCodeStatus } from '$lib/types/device.type';

	interface Props {
		open: boolean;
		onOpenChange: (open: boolean) => void;
		onPaired?: () => void;
	}

	let { open = $bindable(false), onOpenChange, onPaired }: Props = $props();

	let pairing = $state<PairingCodeResponse | null>(null);
	let qrDataUrl = $state<string | null>(null);
	let status = $state<PairingCodeStatus | null>(null);
	let pollHandle = $state<ReturnType<typeof setInterval> | null>(null);
	let error = $state<string | null>(null);
	let isCreating = $state(false);
	let secondsLeft = $state<number>(0);
	let countdownHandle = $state<ReturnType<typeof setInterval> | null>(null);

	const isExpired = $derived(
		status?.status === 'expired' || (pairing != null && secondsLeft <= 0 && status?.status !== 'redeemed')
	);
	const isRedeemed = $derived(status?.status === 'redeemed');

	$effect(() => {
		if (open && pairing == null && !isCreating && error == null) {
			void issueCode();
		}
		if (!open) {
			cleanup();
		}
	});

	onDestroy(cleanup);

	async function issueCode() {
		isCreating = true;
		error = null;
		try {
			const res = await deviceService.createPairingCode();
			pairing = res;
			secondsLeft = Math.max(0, res.expiresInSeconds);
			qrDataUrl = await QRCode.toDataURL(res.qrPayload, {
				margin: 1,
				width: 220,
				errorCorrectionLevel: 'M'
			});
			startPolling(res.id);
			startCountdown();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create pairing code';
		} finally {
			isCreating = false;
		}
	}

	function startPolling(id: string) {
		stopPolling();
		pollHandle = setInterval(async () => {
			try {
				const s = await deviceService.getPairingCodeStatus(id);
				status = s;
				if (s.status === 'redeemed') {
					stopPolling();
					stopCountdown();
					onPaired?.();
				} else if (s.status === 'expired') {
					stopPolling();
					stopCountdown();
				}
			} catch {
				// transient errors are harmless during polling
			}
		}, 2000);
	}

	function stopPolling() {
		if (pollHandle != null) {
			clearInterval(pollHandle);
			pollHandle = null;
		}
	}

	function startCountdown() {
		stopCountdown();
		countdownHandle = setInterval(() => {
			secondsLeft = Math.max(0, secondsLeft - 1);
			if (secondsLeft <= 0) {
				stopCountdown();
			}
		}, 1000);
	}

	function stopCountdown() {
		if (countdownHandle != null) {
			clearInterval(countdownHandle);
			countdownHandle = null;
		}
	}

	function cleanup() {
		stopPolling();
		stopCountdown();
		pairing = null;
		qrDataUrl = null;
		status = null;
		error = null;
		secondsLeft = 0;
	}

	async function regenerate() {
		cleanup();
		await issueCode();
	}

	function close() {
		cleanup();
		onOpenChange(false);
	}

	function copyShortCode() {
		if (!pairing) return;
		void navigator.clipboard.writeText(pairing.shortCodeRaw);
		toast.success('Pairing code copied');
	}

	function formatCountdown(s: number): string {
		const m = Math.floor(s / 60);
		const sec = s % 60;
		return `${m}:${sec.toString().padStart(2, '0')}`;
	}
</script>

<ResponsiveDialog {open} {onOpenChange} contentClass="sm:max-w-md">
	{#snippet title()}
		<div class="flex items-center gap-3">
			<div class="bg-primary/10 text-primary ring-primary/20 flex size-9 shrink-0 items-center justify-center rounded-lg ring-1">
				<SmartphoneIcon class="size-5" />
			</div>
			<div class="flex flex-col gap-0.5">
				<span class="text-xl leading-none">Pair Mobile Device</span>
				<span class="text-muted-foreground text-sm font-normal">Scan in the Arcane mobile app</span>
			</div>
		</div>
	{/snippet}

	<div class="flex flex-col items-center gap-4 py-4">
		{#if error}
			<div
				class="border-destructive/40 bg-destructive/5 text-destructive flex w-full flex-col items-center gap-3 rounded-lg border p-4"
			>
				<AlertTriangleIcon class="size-6" />
				<p class="text-center text-sm">{error}</p>
				<ArcaneButton action="base" tone="outline" size="sm" onclick={regenerate} icon={RefreshIcon} customLabel="Try again" />
			</div>
		{:else if isCreating || pairing == null}
			<div class="flex h-[260px] w-full items-center justify-center">
				<div class="border-primary size-8 animate-spin rounded-full border-4 border-t-transparent"></div>
			</div>
		{:else if isRedeemed}
			<div class="flex w-full flex-col items-center gap-3 py-6 text-center">
				<div
					class="flex size-14 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-600 ring-1 ring-emerald-500/20 dark:text-emerald-400"
				>
					<SuccessIcon class="size-7" />
				</div>
				<h3 class="text-lg font-semibold">Paired with {status?.deviceName ?? 'your device'}</h3>
				<p class="text-muted-foreground text-sm">Your mobile device is now connected.</p>
			</div>
		{:else if isExpired}
			<div class="flex w-full flex-col items-center gap-3 py-6 text-center">
				<div
					class="flex size-14 items-center justify-center rounded-full bg-amber-500/10 text-amber-600 ring-1 ring-amber-500/20 dark:text-amber-400"
				>
					<AlertTriangleIcon class="size-7" />
				</div>
				<h3 class="text-lg font-semibold">Code expired</h3>
				<p class="text-muted-foreground text-sm">Generate a new code to try again.</p>
				<ArcaneButton
					action="base"
					tone="outline-primary"
					size="sm"
					onclick={regenerate}
					icon={RefreshIcon}
					customLabel="New code"
				/>
			</div>
		{:else}
			{#if pairing.serverInsecure}
				<div
					class="flex w-full items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-3 text-xs text-amber-700 dark:text-amber-400"
				>
					<AlertTriangleIcon class="size-4 shrink-0" />
					<span
						>This server is not configured with HTTPS. Pairing tokens will travel over an unencrypted connection. Configure TLS
						before using this in production.</span
					>
				</div>
			{/if}

			<div class="flex flex-col items-center gap-3">
				{#if qrDataUrl}
					<div class="bg-white p-3">
						<img src={qrDataUrl} alt="Pairing QR code" class="block size-[220px]" />
					</div>
				{/if}
				<p class="text-muted-foreground text-center text-xs">Or enter this code manually:</p>
				<div class="flex items-center gap-2">
					<button
						type="button"
						onclick={copyShortCode}
						class="bg-muted hover:bg-muted/70 rounded-md px-4 py-2 font-mono text-2xl tracking-widest transition-colors"
						title="Click to copy"
					>
						{pairing.shortCode}
					</button>
					<CopyButton text={pairing.shortCodeRaw} size="icon" class="size-9" />
				</div>
				<p class="text-muted-foreground text-xs">Expires in {formatCountdown(secondsLeft)}</p>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<div class="flex w-full justify-end gap-2">
			{#if !isCreating && pairing != null && !isRedeemed && !isExpired}
				<ArcaneButton action="base" tone="outline" size="sm" onclick={regenerate} icon={RefreshIcon} customLabel="Regenerate" />
			{/if}
			<ArcaneButton
				action="base"
				tone="outline"
				size="sm"
				onclick={close}
				icon={CloseIcon}
				customLabel={isRedeemed ? 'Done' : 'Cancel'}
			/>
		</div>
	{/snippet}
</ResponsiveDialog>
