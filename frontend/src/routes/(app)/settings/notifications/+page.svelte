<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { NotificationsIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { apnsService } from '#lib/services/apns-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import type { ApnsDevice } from '#lib/types/apns.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	const isReadOnly = $derived(settingsStore.readOnly);
	const canToggleMobilePush = $derived(hasPermission('settings:write', LOCAL_DOCKER_ENVIRONMENT_ID));
	const mobilePushEnabled = $derived(settingsStore.current?.apnsEnabled === true);
	let savingMobilePush = $state(false);
	let mobileDevices = $state<ApnsDevice[]>([]);
	let testingDeviceId = $state<string | null>(null);

	onMount(() => {
		if (mobilePushEnabled) void loadMobileDevices();
	});

	async function handleMobilePushToggle(enabled: boolean) {
		savingMobilePush = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsStore.save({ apnsEnabled: enabled })),
			message: m.common_update_failed({ resource: m.settings() }),
			setLoadingState: (value) => (savingMobilePush = value),
			onSuccess: async () => {
				if (enabled) {
					await loadMobileDevices();
				} else {
					mobileDevices = [];
				}
				toast.success(m.common_update_success({ resource: m.settings() }));
			}
		});
	}

	async function loadMobileDevices() {
		const { data } = await tryCatch(apnsService.getStatus());
		mobileDevices = data?.devices ?? [];
	}

	async function testMobileDevice(device: ApnsDevice) {
		testingDeviceId = device.id;
		const { error } = await tryCatch(apnsService.testDevice(device.id));
		testingDeviceId = null;
		if (error) {
			toast.error(extractApiErrorMessage(error));
			return;
		}
		toast.success(m.notifications_mobile_test_sent({ device: device.label || device.id }));
	}

	async function removeMobileDevice(device: ApnsDevice) {
		const { error } = await tryCatch(apnsService.deleteDevice(device.id));
		if (error) {
			toast.error(extractApiErrorMessage(error));
			return;
		}
		toast.success(m.notifications_mobile_device_removed({ device: device.label || device.id }));
		await loadMobileDevices();
	}
</script>

<SettingsPageLayout
	title={m.notifications_title()}
	description={m.notifications_description()}
	icon={NotificationsIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative w-full min-w-0">
			<SettingsSection>
				<SettingsRow
					for="apnsEnabled"
					label={m.notifications_mobile_push_label()}
					description={m.notifications_mobile_push_description()}
					warningText={m.notifications_mobile_push_external_warning()}
					layout="switch"
					lock={settingsStore.lock('apnsEnabled')}
				>
					<Switch
						id="apnsEnabled"
						checked={mobilePushEnabled}
						disabled={isReadOnly || !canToggleMobilePush || savingMobilePush}
						onCheckedChange={(checked) => void handleMobilePushToggle(checked)}
					/>
				</SettingsRow>
				{#if mobilePushEnabled}
					<SettingsRow label={m.notifications_mobile_devices()} layout="wide">
						{#if mobileDevices.length === 0}
							<p class="text-xs text-muted-foreground">{m.notifications_mobile_devices_empty()}</p>
						{:else}
							<ul class="divide-y divide-border/40">
								{#each mobileDevices as device (device.id)}
									<li class="flex items-center justify-between gap-3 py-2">
										<div class="min-w-0">
											<p class="truncate text-sm">{device.label || device.id}</p>
											{#if device.lastSeenAt}
												<p class="text-xs text-muted-foreground">{formatRelativeTime(device.lastSeenAt)}</p>
											{/if}
										</div>
										<div class="flex shrink-0 items-center gap-2">
											<ArcaneButton
												action="test"
												size="sm"
												loading={testingDeviceId === device.id}
												onclick={() => testMobileDevice(device)}
											/>
											<ArcaneButton action="remove" size="sm" onclick={() => removeMobileDevice(device)} />
										</div>
									</li>
								{/each}
							</ul>
						{/if}
					</SettingsRow>
				{/if}
			</SettingsSection>
		</fieldset>
	{/snippet}
</SettingsPageLayout>
