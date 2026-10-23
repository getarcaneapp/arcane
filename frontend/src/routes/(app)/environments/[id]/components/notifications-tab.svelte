<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { EmptyState } from '#lib/components/states/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { NotificationsIcon, ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { notificationService } from '#lib/services/notification-service.js';
	import { type NotificationProviderKey, NOTIFICATION_PROVIDER_KEYS } from '#lib/types/notifications.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
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
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { BuiltInProviderForm } from './providers';
	import type { NotificationsTabProps } from './tab-props';

	let { environmentId, disabled }: NotificationsTabProps = $props();

	let isLoaded = $state(false);
	let loadError = $state<string | null>(null);
	let isLoading = $state(false);
	let isTesting = $state(false);
	let showUnsavedDialog = $state(false);
	let pendingTestAction: (() => Promise<void>) | null = $state(null);
	let providerTab = $state<NotificationProviderKey>('email');
	const tabItems: TabItem[] = NOTIFICATION_PROVIDER_KEYS.map(
		(provider) =>
			({
				value: provider,
				label: getNotificationProviderDefinition(provider).label()
			}) satisfies TabItem
	);

	type ProviderFormRef = { isValid: () => boolean };

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

	async function loadSettings() {
		loadError = null;
		const result = await tryCatch(notificationService.getSettings(environmentId));
		if (result.error !== null) {
			loadError = extractApiErrorMessage(result.error);
			return;
		}
		savedSettings = createNotificationSettingsByProvider(result.data);
		providerValues = createNotificationProviderFormState(savedSettings);
		providerBaselines = cloneNotificationProviderFormState(providerValues);
		isLoaded = true;
	}

	onMount(() => {
		void loadSettings();
	});

	async function onSubmit() {
		if (NOTIFICATION_PROVIDER_KEYS.some((provider) => providerFormRefs[provider]?.isValid() === false)) {
			toast.error(m.common_form_errors());
			return;
		}

		isLoading = true;
		const errors: string[] = [];
		for (const provider of changedProviders) {
			const settings = notificationProviderFormValuesToSettings(provider, providerValues[provider]);
			const result = await tryCatch(notificationService.updateSettings(provider, settings, environmentId));
			if (result.error !== null) {
				errors.push(
					m.notifications_saved_failed({
						provider: getNotificationProviderDefinition(provider).label(),
						error: extractApiErrorMessage(result.error)
					})
				);
				continue;
			}
			savedSettings = { ...savedSettings, [provider]: result.data };
			providerBaselines = updateNotificationProviderFormState(providerBaselines, provider, providerValues[provider]);
		}
		isLoading = false;

		if (errors.length === 0) {
			toast.success(m.general_settings_saved());
		} else {
			errors.forEach((err) => toast.error(err));
		}
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
		const result = await tryCatch(notificationService.testNotification(provider, testType, environmentId));
		isTesting = false;
		if (result.error !== null) {
			toast.error(m.notifications_test_failed({ error: extractApiErrorMessage(result.error) }));
			return;
		}
		if (result.data?.data?.warning) {
			toast.warning(m.notifications_test_warning({ warning: result.data.data.warning }));
		} else {
			toast.success(m.notifications_test_success({ provider: getNotificationProviderDefinition(provider).label() }));
		}
	}

	async function handleSaveAndTest() {
		showUnsavedDialog = false;
		await onSubmit();
		if (!hasChanges && pendingTestAction) {
			await pendingTestAction();
		}
		pendingTestAction = null;
	}
</script>

{#if loadError}
	<EmptyState
		icon={NotificationsIcon}
		title={m.common_load_failed({ resource: m.notifications_title() })}
		description={loadError}
		actionLabel={m.common_retry()}
		onAction={loadSettings}
	/>
{:else if isLoaded}
	<Tabs.Root value={providerTab} class="flex min-h-0 w-full min-w-0 flex-col">
		<div class="flex flex-wrap items-center justify-between gap-3">
			<TabBar
				items={tabItems}
				value={providerTab}
				onValueChange={(value) => (providerTab = value as NotificationProviderKey)}
				class="self-start"
			/>
			<div class="flex items-center gap-2">
				{#if hasChanges}
					<ArcaneButton
						action="base"
						tone="outline"
						size="sm"
						onclick={resetForm}
						disabled={isLoading}
						class="gap-2"
						icon={ResetIcon}
						customLabel={m.common_reset()}
					/>
				{/if}
				<ArcaneButton
					action="save"
					onclick={onSubmit}
					disabled={disabled || !hasChanges}
					loading={isLoading}
					size="sm"
					class="min-w-20 gap-2"
				/>
			</div>
		</div>

		{#each NOTIFICATION_PROVIDER_KEYS as provider (provider)}
			<Tabs.Content value={provider} class="mt-4">
				<div class="space-y-4">
					<BuiltInProviderForm
						bind:this={providerFormRefs[provider]}
						{provider}
						bind:values={providerValues[provider]}
						{disabled}
						{isTesting}
						hasExistingCredentials={provider === 'matrix'
							? hasSavedCredential(savedSettings.matrix, 'password')
							: savedSettings[provider] !== null}
						hasExistingPassword={provider === 'signal' && hasSavedCredential(savedSettings.signal, 'password')}
						hasExistingToken={provider === 'signal' && hasSavedCredential(savedSettings.signal, 'token')}
						onTest={(testType) => testNotification(provider, testType)}
					/>
				</div>
			</Tabs.Content>
		{/each}
	</Tabs.Root>
{/if}

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
