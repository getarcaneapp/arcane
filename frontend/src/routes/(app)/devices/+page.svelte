<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { SettingsPageLayout } from '$lib/layouts/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Card } from '$lib/components/ui/card';
	import { AddIcon, TrashIcon, EditIcon, SmartphoneIcon, AlertTriangleIcon } from '$lib/icons';
	import PairDeviceDialog from '$lib/components/dialogs/pair-device-dialog.svelte';
	import { deviceService } from '$lib/services/device-service';
	import type { Device } from '$lib/types/device.type';
	import { formatDistanceToNow } from 'date-fns';

	let { data } = $props();

	let devices = $state<Device[]>([...data.devices]);
	let pairOpen = $state(false);
	let renamingId = $state<string | null>(null);
	let renameValue = $state('');

	async function refresh() {
		try {
			devices = await deviceService.listDevices();
		} catch (err) {
			toast.error('Failed to reload devices');
			console.error(err);
		}
	}

	async function revoke(d: Device) {
		if (!confirm(`Revoke "${d.name}"? The device will be signed out on its next request.`)) return;
		try {
			await deviceService.revokeDevice(d.id);
			toast.success(`Revoked ${d.name}`);
			await refresh();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to revoke device');
		}
	}

	function startRename(d: Device) {
		renamingId = d.id;
		renameValue = d.name;
	}

	async function commitRename(d: Device) {
		const next = renameValue.trim();
		renamingId = null;
		if (!next || next === d.name) return;
		try {
			await deviceService.renameDevice(d.id, { name: next });
			toast.success('Renamed');
			await refresh();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to rename device');
		}
	}

	function cancelRename() {
		renamingId = null;
	}

	function lastSeenLabel(d: Device): string {
		if (!d.lastSeenAt) return 'Never';
		try {
			return formatDistanceToNow(new Date(d.lastSeenAt), { addSuffix: true });
		} catch {
			return d.lastSeenAt;
		}
	}

	function pairedLabel(d: Device): string {
		try {
			return formatDistanceToNow(new Date(d.pairedAt), { addSuffix: true });
		} catch {
			return d.pairedAt;
		}
	}
</script>

<SettingsPageLayout
	title="Mobile Devices"
	description="Manage paired mobile devices that can connect to this server."
	icon={SmartphoneIcon}
	pageType="management"
	actionButtons={[
		{
			action: 'create',
			label: 'Pair New Device',
			loadingLabel: 'Pair New Device',
			loading: false,
			disabled: false,
			showOnMobile: true,
			onclick: () => (pairOpen = true)
		}
	]}
>
	{#snippet mainContent()}
		{#if devices.length === 0}
			<Card class="flex flex-col items-center gap-4 px-6 py-14 text-center">
				<div class="bg-primary/10 text-primary ring-primary/20 flex size-12 items-center justify-center rounded-full ring-1">
					<SmartphoneIcon class="size-6" />
				</div>
				<div>
					<h2 class="text-lg font-semibold">No devices paired yet</h2>
					<p class="text-muted-foreground mt-1 text-sm">
						Open the Arcane mobile app and pair it with this server to manage your stacks on the go.
					</p>
				</div>
				<ArcaneButton action="create" customLabel="Pair New Device" icon={AddIcon} onclick={() => (pairOpen = true)} />
			</Card>
		{:else}
			<div class="space-y-3">
				{#each devices as d (d.id)}
					<Card class="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
						<div class="flex min-w-0 items-start gap-3">
							<div
								class="bg-primary/10 text-primary ring-primary/20 flex size-10 shrink-0 items-center justify-center rounded-lg ring-1"
							>
								<SmartphoneIcon class="size-5" />
							</div>
							<div class="min-w-0 flex-1">
								{#if renamingId === d.id}
									<form
										onsubmit={(e) => {
											e.preventDefault();
											void commitRename(d);
										}}
									>
										<input
											type="text"
											bind:value={renameValue}
											autofocus
											onblur={() => commitRename(d)}
											onkeydown={(e) => {
												if (e.key === 'Escape') cancelRename();
											}}
											class="bg-background border-input w-full max-w-xs rounded-md border px-2 py-1 text-sm font-semibold"
										/>
									</form>
								{:else}
									<button
										type="button"
										onclick={() => startRename(d)}
										class="text-left text-base font-semibold hover:underline"
										title="Click to rename"
									>
										{d.name}
									</button>
								{/if}
								<div class="text-muted-foreground mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-xs">
									<span>Paired {pairedLabel(d)}</span>
									<span>Last seen {lastSeenLabel(d)}</span>
									{#if d.deviceModel}
										<span>{d.deviceModel}</span>
									{/if}
									{#if d.osVersion}
										<span>iOS {d.osVersion}</span>
									{/if}
									{#if d.appVersion}
										<span>v{d.appVersion}</span>
									{/if}
								</div>
							</div>
						</div>
						<div class="flex shrink-0 items-center gap-2">
							<ArcaneButton
								action="base"
								tone="outline"
								size="sm"
								icon={EditIcon}
								customLabel="Rename"
								onclick={() => startRename(d)}
							/>
							<ArcaneButton
								action="delete"
								tone="outline"
								size="sm"
								icon={TrashIcon}
								customLabel="Revoke"
								onclick={() => revoke(d)}
							/>
						</div>
					</Card>
				{/each}
			</div>
		{/if}
	{/snippet}
</SettingsPageLayout>

<PairDeviceDialog
	bind:open={pairOpen}
	onOpenChange={(o) => (pairOpen = o)}
	onPaired={() => {
		void refresh();
	}}
/>
