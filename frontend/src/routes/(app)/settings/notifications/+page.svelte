<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Dialog from '$lib/components/ui/dialog';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { toast } from 'svelte-sonner';
	import { getContext, onMount } from 'svelte';
	import { SettingsPageLayout } from '$lib/layouts';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { notificationService } from '$lib/services/notification-service';
	import type { NotificationSettings } from '$lib/types/notification.type';
	import {
		type DiscordFormValues,
		type EmailFormValues,
		type TelegramFormValues,
		type SignalFormValues,
		type SlackFormValues,
		type NtfyFormValues,
		type PushoverFormValues,
		type GotifyFormValues,
		type GenericFormValues,
		type AppriseFormValues,
		type NotificationProviderKey,
		NOTIFICATION_PROVIDER_KEYS,
		discordSettingsToFormValues,
		emailSettingsToFormValues,
		telegramSettingsToFormValues,
		signalSettingsToFormValues,
		slackSettingsToFormValues,
		ntfySettingsToFormValues,
		pushoverSettingsToFormValues,
		gotifySettingsToFormValues,
		genericSettingsToFormValues,
		appriseSettingsToFormValues,
		discordFormValuesToSettings,
		emailFormValuesToSettings,
		telegramFormValuesToSettings,
		signalFormValuesToSettings,
		slackFormValuesToSettings,
		ntfyFormValuesToSettings,
		pushoverFormValuesToSettings,
		gotifyFormValuesToSettings,
		genericFormValuesToSettings,
		appriseFormValuesToSettings
	} from '$lib/types/notification-providers';
	import { NotificationsIcon } from '$lib/icons';
	import {
		EmailProviderForm,
		DiscordProviderForm,
		TelegramProviderForm,
		SignalProviderForm,
		SlackProviderForm,
		NtfyProviderForm,
		PushoverProviderForm,
		GotifyProviderForm,
		GenericProviderForm,
		AppriseProviderForm
	} from './providers';

	let { data } = $props();

	// UI state
	let isLoading = $state(false);
	let isTesting = $state(false);
	let showUnsavedDialog = $state(false);
	let pendingTestAction: (() => Promise<void>) | null = $state(null);
	let notificationsTab = $state<'built-in' | 'apprise'>('built-in');
	let providerTab = $state<NotificationProviderKey>('email');

	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);

	// Settings form context
	let formState: any = null;
	try {
		formState = getContext('settingsFormState') as any;
	} catch {
		// Context not available (shouldn't happen in settings routes)
	}

	// Provider form references for validation
	let emailFormRef: EmailProviderForm;
	let discordFormRef: DiscordProviderForm;
	let telegramFormRef: TelegramProviderForm;
	let signalFormRef: SignalProviderForm;
	let slackFormRef: SlackProviderForm;
	let ntfyFormRef: NtfyProviderForm;
	let pushoverFormRef: PushoverProviderForm;
	let gotifyFormRef: GotifyProviderForm;
	let genericFormRef: GenericProviderForm;

	// Saved settings from server (used to detect if settings exist)
	let savedSettings = $state<Record<NotificationProviderKey, NotificationSettings | null>>({
		email: null,
		discord: null,
		telegram: null,
		signal: null,
		slack: null,
		ntfy: null,
		pushover: null,
		gotify: null,
		generic: null
	});

	// Current form values - these are what the user edits
	let emailValues = $state<EmailFormValues>(emailSettingsToFormValues());
	let discordValues = $state<DiscordFormValues>(discordSettingsToFormValues());
	let telegramValues = $state<TelegramFormValues>(telegramSettingsToFormValues());
	let signalValues = $state<SignalFormValues>(signalSettingsToFormValues());
	let slackValues = $state<SlackFormValues>(slackSettingsToFormValues());
	let ntfyValues = $state<NtfyFormValues>(ntfySettingsToFormValues());
	let pushoverValues = $state<PushoverFormValues>(pushoverSettingsToFormValues());
	let gotifyValues = $state<GotifyFormValues>(gotifySettingsToFormValues());
	let genericValues = $state<GenericFormValues>(genericSettingsToFormValues());
	let appriseValues = $state<AppriseFormValues>(appriseSettingsToFormValues());

	// Baseline values - what was last saved (for change detection)
	let emailBaseline = $state<EmailFormValues>(emailSettingsToFormValues());
	let discordBaseline = $state<DiscordFormValues>(discordSettingsToFormValues());
	let telegramBaseline = $state<TelegramFormValues>(telegramSettingsToFormValues());
	let signalBaseline = $state<SignalFormValues>(signalSettingsToFormValues());
	let slackBaseline = $state<SlackFormValues>(slackSettingsToFormValues());
	let ntfyBaseline = $state<NtfyFormValues>(ntfySettingsToFormValues());
	let pushoverBaseline = $state<PushoverFormValues>(pushoverSettingsToFormValues());
	let gotifyBaseline = $state<GotifyFormValues>(gotifySettingsToFormValues());
	let genericBaseline = $state<GenericFormValues>(genericSettingsToFormValues());
	let appriseBaseline = $state<AppriseFormValues>(appriseSettingsToFormValues());

	// Change detection
	const emailHasChanges = $derived(JSON.stringify(emailValues) !== JSON.stringify(emailBaseline));
	const discordHasChanges = $derived(JSON.stringify(discordValues) !== JSON.stringify(discordBaseline));
	const telegramHasChanges = $derived(JSON.stringify(telegramValues) !== JSON.stringify(telegramBaseline));
	const signalHasChanges = $derived(JSON.stringify(signalValues) !== JSON.stringify(signalBaseline));
	const slackHasChanges = $derived(JSON.stringify(slackValues) !== JSON.stringify(slackBaseline));
	const ntfyHasChanges = $derived(JSON.stringify(ntfyValues) !== JSON.stringify(ntfyBaseline));
	const pushoverHasChanges = $derived(JSON.stringify(pushoverValues) !== JSON.stringify(pushoverBaseline));
	const gotifyHasChanges = $derived(JSON.stringify(gotifyValues) !== JSON.stringify(gotifyBaseline));
	const genericHasChanges = $derived(JSON.stringify(genericValues) !== JSON.stringify(genericBaseline));
	const appriseHasChanges = $derived(JSON.stringify(appriseValues) !== JSON.stringify(appriseBaseline));
	const hasChanges = $derived(
		emailHasChanges ||
			discordHasChanges ||
			telegramHasChanges ||
			signalHasChanges ||
			slackHasChanges ||
			ntfyHasChanges ||
			pushoverHasChanges ||
			gotifyHasChanges ||
			genericHasChanges ||
			appriseHasChanges
	);

	// Sync with settings form context
	$effect(() => {
		if (formState) {
			formState.hasChanges = hasChanges;
			formState.isLoading = isLoading;
			formState.saveFunction = onSubmit;
			formState.resetFunction = resetForm;
		}
	});

	// Load initial data
	onMount(async () => {
		// Load built-in provider settings
		for (const provider of NOTIFICATION_PROVIDER_KEYS) {
			const found = data?.notificationSettings?.find((s: NotificationSettings) => s.provider === provider);
			savedSettings[provider] = found ?? null;
		}

		// Apply saved values to form
		emailValues = emailSettingsToFormValues(savedSettings.email ?? undefined);
		emailBaseline = { ...emailValues };

		discordValues = discordSettingsToFormValues(savedSettings.discord ?? undefined);
		discordBaseline = { ...discordValues };

		telegramValues = telegramSettingsToFormValues(savedSettings.telegram ?? undefined);
		telegramBaseline = { ...telegramValues };

		signalValues = signalSettingsToFormValues(savedSettings.signal ?? undefined);
		signalBaseline = { ...signalValues };

		slackValues = slackSettingsToFormValues(savedSettings.slack ?? undefined);
		slackBaseline = { ...slackValues };

		ntfyValues = ntfySettingsToFormValues(savedSettings.ntfy ?? undefined);
		ntfyBaseline = { ...ntfyValues };

		pushoverValues = pushoverSettingsToFormValues(savedSettings.pushover ?? undefined);
		pushoverBaseline = { ...pushoverValues };

		gotifyValues = gotifySettingsToFormValues(savedSettings.gotify ?? undefined);
		gotifyBaseline = { ...gotifyValues };

		genericValues = genericSettingsToFormValues(savedSettings.generic ?? undefined);
		genericBaseline = { ...genericValues };

		// Load Apprise settings
		try {
			const settings = await notificationService.getAppriseSettings();
			appriseValues = appriseSettingsToFormValues(settings);
			appriseBaseline = { ...appriseValues };
		} catch {
			// Apprise not configured yet, keep defaults
		}
	});

	async function onSubmit() {
		// Validate all forms
		const emailValid = emailFormRef?.isValid() ?? true;
		const discordValid = discordFormRef?.isValid() ?? true;
		const telegramValid = telegramFormRef?.isValid() ?? true;
		const signalValid = signalFormRef?.isValid() ?? true;
		const slackValid = slackFormRef?.isValid() ?? true;
		const ntfyValid = ntfyFormRef?.isValid() ?? true;
		const pushoverValid = pushoverFormRef?.isValid() ?? true;
		const gotifyValid = gotifyFormRef?.isValid() ?? true;
		const genericValid = genericFormRef?.isValid() ?? true;

		if (
			!(
				emailValid &&
				discordValid &&
				telegramValid &&
				signalValid &&
				slackValid &&
				ntfyValid &&
				pushoverValid &&
				gotifyValid &&
				genericValid
			)
		) {
			toast.error('Please check the form for errors');
			return;
		}

		isLoading = true;

		try {
			const errors: string[] = [];

			// Save Email settings if changed
			if (emailHasChanges) {
				try {
					const settings = emailFormValuesToSettings(emailValues);
					await notificationService.updateSettings('email', settings);
					savedSettings.email = settings;
					emailBaseline = { ...emailValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Email', error: errorMsg }));
				}
			}

			// Save Discord settings if changed
			if (discordHasChanges) {
				try {
					const settings = discordFormValuesToSettings(discordValues);
					await notificationService.updateSettings('discord', settings);
					savedSettings.discord = settings;
					discordBaseline = { ...discordValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Discord', error: errorMsg }));
				}
			}

			// Save Telegram settings if changed
			if (telegramHasChanges) {
				try {
					const settings = telegramFormValuesToSettings(telegramValues);
					await notificationService.updateSettings('telegram', settings);
					savedSettings.telegram = settings;
					telegramBaseline = { ...telegramValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Telegram', error: errorMsg }));
				}
			}

			// Save Signal settings if changed
			if (signalHasChanges) {
				try {
					const settings = signalFormValuesToSettings(signalValues);
					await notificationService.updateSettings('signal', settings);
					savedSettings.signal = settings;
					signalBaseline = { ...signalValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Signal', error: errorMsg }));
				}
			}

			// Save Slack settings if changed
			if (slackHasChanges) {
				try {
					const settings = slackFormValuesToSettings(slackValues);
					await notificationService.updateSettings('slack', settings);
					savedSettings.slack = settings;
					slackBaseline = { ...slackValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Slack', error: errorMsg }));
				}
			}

			// Save Ntfy settings if changed
			if (ntfyHasChanges) {
				try {
					const settings = ntfyFormValuesToSettings(ntfyValues);
					await notificationService.updateSettings('ntfy', settings);
					savedSettings.ntfy = settings;
					ntfyBaseline = { ...ntfyValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Ntfy', error: errorMsg }));
				}
			}

			// Save Pushover settings if changed
			if (pushoverHasChanges) {
				try {
					const settings = pushoverFormValuesToSettings(pushoverValues);
					await notificationService.updateSettings('pushover', settings);
					savedSettings.pushover = settings;
					pushoverBaseline = { ...pushoverValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Pushover', error: errorMsg }));
				}
			}

			// Save Gotify settings if changed
			if (gotifyHasChanges) {
				try {
					const settings = gotifyFormValuesToSettings(gotifyValues);
					await notificationService.updateSettings('gotify', settings);
					savedSettings.gotify = settings;
					gotifyBaseline = { ...gotifyValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Gotify', error: errorMsg }));
				}
			}

			// Save Generic settings if changed
			if (genericHasChanges) {
				try {
					const settings = genericFormValuesToSettings(genericValues);
					await notificationService.updateSettings('generic', settings);
					savedSettings.generic = settings;
					genericBaseline = { ...genericValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
					errors.push(m.notifications_saved_failed({ provider: 'Generic', error: errorMsg }));
				}
			}

			// Save Apprise settings if changed
			if (appriseHasChanges) {
				try {
					const settings = appriseFormValuesToSettings(appriseValues);
					await notificationService.updateAppriseSettings(settings);
					appriseBaseline = { ...appriseValues };
				} catch (error: any) {
					const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
					errors.push(m.notifications_saved_failed({ provider: 'Apprise', error: errorMsg }));
				}
			}

			if (errors.length === 0) {
				toast.success(m.general_settings_saved());
			} else {
				errors.forEach((err) => toast.error(err));
			}
		} catch (error) {
			console.error('Error saving notification settings:', error);
			toast.error('Failed to save notification settings. Please try again.');
		} finally {
			isLoading = false;
		}
	}

	function resetForm() {
		emailValues = { ...emailBaseline };
		discordValues = { ...discordBaseline };
		telegramValues = { ...telegramBaseline };
		signalValues = { ...signalBaseline };
		slackValues = { ...slackBaseline };
		ntfyValues = { ...ntfyBaseline };
		pushoverValues = { ...pushoverBaseline };
		gotifyValues = { ...gotifyBaseline };
		genericValues = { ...genericBaseline };
		appriseValues = { ...appriseBaseline };
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
			await notificationService.testNotification(provider, testType);
			toast.success(m.notifications_test_success({ provider: provider.charAt(0).toUpperCase() + provider.slice(1) }));
		} catch (error: any) {
			const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
			toast.error(m.notifications_test_failed({ error: errorMsg }));
		} finally {
			isTesting = false;
		}
	}

	async function testAppriseNotification() {
		if (hasChanges) {
			pendingTestAction = executeAppriseTest;
			showUnsavedDialog = true;
			return;
		}
		await executeAppriseTest();
	}

	async function executeAppriseTest() {
		isTesting = true;
		try {
			await notificationService.testAppriseNotification();
			toast.success(m.notifications_test_success({ provider: 'Apprise' }));
		} catch (error: any) {
			const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
			toast.error(m.notifications_test_failed({ error: errorMsg }));
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
		<fieldset disabled={isReadOnly} class="relative">
			{#if isReadOnly}
				<Alert.Root variant="default" class="mb-4 sm:mb-6">
					<Alert.Title>{m.notifications_read_only_title()}</Alert.Title>
					<Alert.Description>{m.notifications_read_only_description()}</Alert.Description>
				</Alert.Root>
			{/if}

			<Tabs.Root bind:value={notificationsTab}>
				<Tabs.List class="inline-flex w-auto">
					<Tabs.Trigger value="built-in">{m.notifications_tab_built_in()}</Tabs.Trigger>
					<Tabs.Trigger value="apprise">{m.notifications_tab_apprise()}</Tabs.Trigger>
				</Tabs.List>

				<Tabs.Content value="built-in" class="mt-4 sm:mt-6">
					<div class="space-y-8">
						<Tabs.Root bind:value={providerTab}>
							<Tabs.List class="inline-flex w-auto">
								{#each NOTIFICATION_PROVIDER_KEYS as provider (provider)}
									<Tabs.Trigger value={provider}>{provider.charAt(0).toUpperCase() + provider.slice(1)}</Tabs.Trigger>
								{/each}
							</Tabs.List>

							<Tabs.Content value="email" class="mt-4 space-y-4">
								<EmailProviderForm
									bind:this={emailFormRef}
									bind:values={emailValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={(testType) => testNotification('email', testType)}
								/>
							</Tabs.Content>

							<Tabs.Content value="discord" class="mt-4 space-y-4">
								<DiscordProviderForm
									bind:this={discordFormRef}
									bind:values={discordValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('discord')}
								/>
							</Tabs.Content>

							<Tabs.Content value="telegram" class="mt-4 space-y-4">
								<TelegramProviderForm
									bind:this={telegramFormRef}
									bind:values={telegramValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('telegram')}
								/>
							</Tabs.Content>

							<Tabs.Content value="signal" class="mt-4 space-y-4">
								<SignalProviderForm
									bind:this={signalFormRef}
									bind:values={signalValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('signal')}
								/>
							</Tabs.Content>

							<Tabs.Content value="slack" class="mt-4 space-y-4">
								<SlackProviderForm
									bind:this={slackFormRef}
									bind:values={slackValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('slack')}
								/>
							</Tabs.Content>

							<Tabs.Content value="ntfy" class="mt-4 space-y-4">
								<NtfyProviderForm
									bind:this={ntfyFormRef}
									bind:values={ntfyValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('ntfy')}
								/>
							</Tabs.Content>

							<Tabs.Content value="pushover" class="mt-4 space-y-4">
								<PushoverProviderForm
									bind:this={pushoverFormRef}
									bind:values={pushoverValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('pushover')}
								/>
							</Tabs.Content>

							<Tabs.Content value="gotify" class="mt-4 space-y-4">
								<GotifyProviderForm
									bind:this={gotifyFormRef}
									bind:values={gotifyValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('gotify')}
								/>
							</Tabs.Content>

							<Tabs.Content value="generic" class="mt-4 space-y-4">
								<GenericProviderForm
									bind:this={genericFormRef}
									bind:values={genericValues}
									disabled={isReadOnly}
									{isTesting}
									onTest={() => testNotification('generic')}
								/>
							</Tabs.Content>
						</Tabs.Root>
					</div>
				</Tabs.Content>

				<Tabs.Content value="apprise" class="mt-4 space-y-4 sm:mt-6 sm:space-y-6">
					<Alert.Root variant="default" class="border-yellow-500/50 bg-yellow-500/10 text-yellow-600 dark:text-yellow-400">
						<Alert.Title>Deprecated</Alert.Title>
						<Alert.Description>
							Apprise support is deprecated and will be removed in a future release. Please migrate to the built-in notification
							providers.
						</Alert.Description>
					</Alert.Root>

					<AppriseProviderForm bind:values={appriseValues} disabled={isReadOnly} {isTesting} onTest={testAppriseNotification} />
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
