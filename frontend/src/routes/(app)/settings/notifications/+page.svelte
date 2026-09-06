<script lang="ts">
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { toast } from 'svelte-sonner';
	import { getContext, onMount } from 'svelte';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import settingsStore from '#lib/stores/config-store.js';
	import { m } from '#lib/paraglide/messages.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { notificationService } from '#lib/services/notification-service.js';
	import { type NotificationProviderKey, NOTIFICATION_PROVIDER_KEYS } from '#lib/types/notifications.js';
	import { AlertIcon, NotificationsIcon } from '#lib/icons/index.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import type { Settings } from '#lib/types/settings.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import { BuiltInProviderForm } from './providers';
	import {
		cloneNotificationProviderFormState,
		createNotificationProviderFormState,
		createNotificationSettingsByProvider,
		getNotificationProviderDefinition,
		notificationProviderFormValuesToSettings,
		type NotificationProviderFormState,
		type NotificationSettingsByProvider,
		updateNotificationProviderFormState
	} from '#lib/utils/notification-providers.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks, tryCatch } from '#lib/utils/api.js';
	import { apnsService } from '#lib/services/apns-service.js';
	import type { ApnsDevice } from '#lib/types/apns.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';

	let { data } = $props();

	// UI state
	let isLoading = $state(false);
	let isTesting = $state(false);
	let showUnsavedDialog = $state(false);
	let pendingTestAction: (() => Promise<void>) | null = $state(null);
	type NotificationTab = NotificationProviderKey | 'mobile';
	const NOTIFICATION_TABS: readonly NotificationTab[] = [...NOTIFICATION_PROVIDER_KEYS, 'mobile'];
	const urlTab = useUrlTab<NotificationTab>({
		validTabs: () => NOTIFICATION_TABS,
		defaultTab: () => 'email'
	});
	const providerTab = $derived(urlTab.value);
	const tabItems: TabItem[] = [
		...NOTIFICATION_PROVIDER_KEYS.map(
			(provider) =>
				({
					value: provider,
					label: getNotificationProviderDefinition(provider).label()
				}) satisfies TabItem
		),
		{ value: 'mobile', label: m.notifications_mobile_push_tab() }
	];

	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const canToggleMobilePush = $derived(hasPermission('settings:write'));
	const mobilePushEnabled = $derived($settingsStore?.apnsEnabled === true);
	let savingMobilePush = $state(false);
	let mobileDevices = $state<ApnsDevice[]>([]);
	let testingDeviceId = $state<string | null>(null);

	type SettingsFormState = {
		hasChanges: boolean;
		isLoading: boolean;
		saveFunction: (() => Promise<void>) | null;
		resetFunction: (() => void) | null;
	};
	type ProviderFormRef = { isValid: () => boolean };

	const formState = getContext<SettingsFormState | undefined>('settingsFormState');
	let providerFormRefs = $state<Partial<Record<NotificationProviderKey, ProviderFormRef>>>({});
	let savedSettings = $state<NotificationSettingsByProvider>(createNotificationSettingsByProvider());
	let providerValues = $state<NotificationProviderFormState>(createNotificationProviderFormState());
	let providerBaselines = $state<NotificationProviderFormState>(createNotificationProviderFormState());
	const changedProviders = $derived.by(() =>
		NOTIFICATION_PROVIDER_KEYS.filter(
			(provider) => JSON.stringify(providerValues[provider]) !== JSON.stringify(providerBaselines[provider])
		)
	);
	const hasChanges = $derived(changedProviders.length > 0);

	function hasSavedCredential(settings: NotificationSettingsByProvider[NotificationProviderKey], field: string) {
		return settings?.config ? Object.prototype.hasOwnProperty.call(settings.config, field) : false;
	}

	// Sync with settings form context
	$effect(() => {
		if (formState) {
			formState.hasChanges = hasChanges;
			formState.isLoading = isLoading;
			formState.saveFunction = onSubmit;
			formState.resetFunction = resetForm;
		}
	});

	onMount(() => {
		savedSettings = createNotificationSettingsByProvider(data?.notificationSettings ?? []);
		providerValues = createNotificationProviderFormState(savedSettings);
		providerBaselines = cloneNotificationProviderFormState(providerValues);
		if (mobilePushEnabled) void loadMobileDevices();
	});

	async function onSubmit() {
		if (NOTIFICATION_PROVIDER_KEYS.some((provider) => providerFormRefs[provider]?.isValid() === false)) {
			toast.error(m.common_form_errors());
			return;
		}

		isLoading = true;

		try {
			const errors: string[] = [];
			for (const provider of changedProviders) {
				try {
					const settings = notificationProviderFormValuesToSettings(provider, providerValues[provider]);
					const saved = await notificationService.updateSettings(provider, settings);
					savedSettings = { ...savedSettings, [provider]: saved };
					providerBaselines = updateNotificationProviderFormState(providerBaselines, provider, providerValues[provider]);
				} catch (error) {
					errors.push(
						m.notifications_saved_failed({
							provider: getNotificationProviderDefinition(provider).label(),
							error: extractApiErrorMessage(error)
						})
					);
				}
			}

			if (errors.length === 0) {
				toast.success(m.general_settings_saved());
			} else {
				errors.forEach((err) => toast.error(err));
			}
		} catch (error) {
			console.error('Error saving notification settings:', error);
			toast.error(m.settings_notifications_save_error());
		} finally {
			isLoading = false;
		}
	}

	async function handleMobilePushToggle(enabled: boolean) {
		handleApiResultWithCallbacks<Settings>({
			result: await tryCatch(settingsService.updateSettings({ apnsEnabled: enabled })),
			message: m.common_update_failed({ resource: m.settings() }),
			setLoadingState: (value) => (savingMobilePush = value),
			onSuccess: async (updated) => {
				settingsStore.set(updated);
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

	function resetForm() {
		providerValues = cloneNotificationProviderFormState(providerBaselines);
	}

	async function testNotification(provider: NotificationProviderKey, testType: string = 'simple') {
		if (hasChanges) {
			pendingTestAction = () => executeTest(provider, testType);
			showUnsavedDialog = true;
			return;
		}
		await executeTest(provider, testType);
	}

	async function executeTest(provider: NotificationProviderKey, testType: string = 'simple') {
		isTesting = true;
		try {
			const result = await notificationService.testNotification(provider, testType);
			if (result?.data?.warning) {
				toast.warning(m.notifications_test_warning({ warning: result.data.warning }));
			} else {
				toast.success(m.notifications_test_success({ provider: getNotificationProviderDefinition(provider).label() }));
			}
		} catch (error) {
			toast.error(m.notifications_test_failed({ error: extractApiErrorMessage(error) }));
		} finally {
			isTesting = false;
		}
	}

	async function handleSaveAndTest() {
		showUnsavedDialog = false;
		await onSubmit();
		if (pendingTestAction) {
			await pendingTestAction();
			pendingTestAction = null;
		}
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
			<Tabs.Root value={providerTab} class="flex min-h-0 w-full min-w-0 flex-col">
				<TabBar items={tabItems} value={providerTab} onValueChange={urlTab.select} class="self-start" />

				{#each NOTIFICATION_PROVIDER_KEYS as provider (provider)}
					<Tabs.Content value={provider} class="mt-4 space-y-4">
						<BuiltInProviderForm
							bind:this={providerFormRefs[provider]}
							{provider}
							bind:values={providerValues[provider]}
							disabled={isReadOnly}
							{isTesting}
							hasExistingCredentials={savedSettings[provider] !== null}
							hasExistingPassword={provider === 'signal' && hasSavedCredential(savedSettings.signal, 'password')}
							hasExistingToken={provider === 'signal' && hasSavedCredential(savedSettings.signal, 'token')}
							onTest={(testType) => testNotification(provider, testType)}
						/>
					</Tabs.Content>
				{/each}
				<Tabs.Content value="mobile" class="mt-4 space-y-4">
					<SettingsRow
						label={m.notifications_mobile_push_label()}
						description={m.notifications_mobile_push_description()}
						layout="inline"
					>
						<Switch
							id="apnsEnabled"
							checked={mobilePushEnabled}
							disabled={isReadOnly || !canToggleMobilePush || savingMobilePush}
							onCheckedChange={(checked) => void handleMobilePushToggle(checked)}
						/>
					</SettingsRow>
					{#if mobilePushEnabled}
						<div class="space-y-2">
							<p class="text-sm font-medium">{m.notifications_mobile_devices()}</p>
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
						</div>
					{/if}
					<Alert.Root variant="warning" class="py-2 [&>svg]:top-2">
						<AlertIcon class="size-4" />
						<Alert.Description class="text-xs">{m.notifications_mobile_push_external_warning()}</Alert.Description>
					</Alert.Root>
				</Tabs.Content>
			</Tabs.Root>
		</fieldset>
	{/snippet}
</SettingsPageLayout>

<Dialog.Root bind:open={showUnsavedDialog}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>{m.notifications_unsaved_changes_title()}</Dialog.Title>
			<Dialog.Description>
				{m.notifications_unsaved_changes_description()}
			</Dialog.Description>
		</Dialog.Header>
		<Dialog.Footer>
			<ArcaneButton action="cancel" onclick={() => (showUnsavedDialog = false)} />
			<ArcaneButton action="confirm" onclick={handleSaveAndTest} customLabel={m.notifications_unsaved_changes_save_and_test()} />
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
