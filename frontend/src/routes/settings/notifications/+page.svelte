<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Alert from '$lib/components/ui/alert';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import { toast } from 'svelte-sonner';
	import { getContext, onMount } from 'svelte';
	import { z } from 'zod/v4';
	import { createForm } from '$lib/utils/form.utils';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { SettingsPageLayout } from '$lib/layouts';
	import BellIcon from '@lucide/svelte/icons/bell';
	import SendIcon from '@lucide/svelte/icons/send';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import settingsStore from '$lib/stores/config-store';
	import { Label } from '$lib/components/ui/label';
	import { Input } from '$lib/components/ui/input';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import { m } from '$lib/paraglide/messages';
	import { notificationService } from '$lib/services/notification-service';
	import type { EmailTLSMode, AppriseSettings } from '$lib/types/notification.type';

	interface FormNotificationSettings {
		discordEnabled: boolean;
		discordWebhookUrl: string;
		discordUsername: string;
		discordAvatarUrl: string;
		discordEventImageUpdate: boolean;
		discordEventContainerUpdate: boolean;
		emailEnabled: boolean;
		emailSmtpHost: string;
		emailSmtpPort: number;
		emailSmtpUsername: string;
		emailSmtpPassword: string;
		emailFromAddress: string;
		emailToAddresses: string;
		emailTlsMode: EmailTLSMode;
		emailEventImageUpdate: boolean;
		emailEventContainerUpdate: boolean;
	}

	let { data } = $props();
	let hasChanges = $state(false);
	let isLoading = $state(false);
	let isTesting = $state(false);
	let showUnsavedDialog = $state(false);
	let pendingTestAction: (() => Promise<void>) | null = $state(null);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const formState = getContext('settingsFormState') as any;

	let appriseSettings: AppriseSettings = $state({
		apiUrl: '',
		enabled: false,
		imageUpdateTag: '',
		containerUpdateTag: ''
	});

	let savedAppriseSettings: AppriseSettings = $state({
		apiUrl: '',
		enabled: false,
		imageUpdateTag: '',
		containerUpdateTag: ''
	});

	const hasUnsavedChanges = $derived.by(() => {
		const formData = form.validate();
		if (!formData) return false;

		const formChanged = JSON.stringify(formData) !== JSON.stringify(currentSettings);
		const appriseChanged = JSON.stringify(appriseSettings) !== JSON.stringify(savedAppriseSettings);
		return formChanged || appriseChanged;
	});
	let currentSettings = $state<FormNotificationSettings>({
		discordEnabled: false,
		discordWebhookUrl: '',
		discordUsername: 'Arcane',
		discordAvatarUrl: '',
		discordEventImageUpdate: true,
		discordEventContainerUpdate: true,
		emailEnabled: false,
		emailSmtpHost: '',
		emailSmtpPort: 587,
		emailSmtpUsername: '',
		emailSmtpPassword: '',
		emailFromAddress: '',
		emailToAddresses: '',
		emailTlsMode: 'starttls',
		emailEventImageUpdate: true,
		emailEventContainerUpdate: true
	});

	const formSchema = z
		.object({
			discordEnabled: z.boolean(),
			discordWebhookUrl: z.url().or(z.literal('')),
			discordUsername: z.string(),
			discordAvatarUrl: z.string(),
			discordEventImageUpdate: z.boolean(),
			discordEventContainerUpdate: z.boolean(),
			emailEnabled: z.boolean(),
			emailSmtpHost: z.string(),
			emailSmtpPort: z.number().int().min(1).max(65535),
			emailSmtpUsername: z.string(),
			emailSmtpPassword: z.string(),
			emailFromAddress: z.email().or(z.literal('')),
			emailToAddresses: z.string(),
			emailTlsMode: z.enum(['none', 'starttls', 'ssl']),
			emailEventImageUpdate: z.boolean(),
			emailEventContainerUpdate: z.boolean()
		})
		.superRefine((data, ctx) => {
			// Validate Discord fields when Discord is enabled
			if (data.discordEnabled && !data.discordWebhookUrl.trim()) {
				ctx.addIssue({
					code: 'custom',
					message: 'Webhook URL is required when Discord is enabled',
					path: ['discordWebhookUrl']
				});
			} // Validate Email fields when Email is enabled
			if (data.emailEnabled) {
				if (!data.emailSmtpHost.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: 'SMTP host is required when email is enabled',
						path: ['emailSmtpHost']
					});
				}

				if (!data.emailFromAddress.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: 'From address is required when email is enabled',
						path: ['emailFromAddress']
					});
				} else {
					// Validate email format using Zod's built-in email validator
					const emailValidation = z.string().email().safeParse(data.emailFromAddress.trim());
					if (!emailValidation.success) {
						ctx.addIssue({
							code: 'custom',
							message: 'Invalid email address format',
							path: ['emailFromAddress']
						});
					}
				}

				if (!data.emailToAddresses.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: 'At least one recipient address is required when email is enabled',
						path: ['emailToAddresses']
					});
				} else {
					// Validate each email in the comma-separated list
					const addresses = data.emailToAddresses
						.split(',')
						.map((addr) => addr.trim())
						.filter((addr) => addr.length > 0);
					const invalidAddresses: string[] = [];

					addresses.forEach((addr) => {
						const emailValidation = z.string().email().safeParse(addr);
						if (!emailValidation.success) {
							invalidAddresses.push(addr);
						}
					});

					if (invalidAddresses.length > 0) {
						ctx.addIssue({
							code: 'custom',
							message: `Invalid email addresses: ${invalidAddresses.join(', ')}`,
							path: ['emailToAddresses']
						});
					}
				}
			}
		});

	let { inputs: formInputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, currentSettings));

	const formHasChanges = $derived.by(
		() =>
			$formInputs.discordEnabled.value !== currentSettings.discordEnabled ||
			$formInputs.discordWebhookUrl.value !== currentSettings.discordWebhookUrl ||
			$formInputs.discordUsername.value !== currentSettings.discordUsername ||
			$formInputs.discordAvatarUrl.value !== currentSettings.discordAvatarUrl ||
			$formInputs.discordEventImageUpdate.value !== currentSettings.discordEventImageUpdate ||
			$formInputs.discordEventContainerUpdate.value !== currentSettings.discordEventContainerUpdate ||
			$formInputs.emailEnabled.value !== currentSettings.emailEnabled ||
			$formInputs.emailSmtpHost.value !== currentSettings.emailSmtpHost ||
			$formInputs.emailSmtpPort.value !== currentSettings.emailSmtpPort ||
			$formInputs.emailSmtpUsername.value !== currentSettings.emailSmtpUsername ||
			$formInputs.emailSmtpPassword.value !== currentSettings.emailSmtpPassword ||
			$formInputs.emailFromAddress.value !== currentSettings.emailFromAddress ||
			$formInputs.emailToAddresses.value !== currentSettings.emailToAddresses ||
			$formInputs.emailTlsMode.value !== currentSettings.emailTlsMode ||
			$formInputs.emailEventImageUpdate.value !== currentSettings.emailEventImageUpdate ||
			$formInputs.emailEventContainerUpdate.value !== currentSettings.emailEventContainerUpdate ||
			appriseSettings.enabled !== savedAppriseSettings.enabled ||
			appriseSettings.apiUrl !== savedAppriseSettings.apiUrl ||
			appriseSettings.imageUpdateTag !== savedAppriseSettings.imageUpdateTag ||
			appriseSettings.containerUpdateTag !== savedAppriseSettings.containerUpdateTag
	);

	$effect(() => {
		hasChanges = formHasChanges;
		if (formState) {
			formState.hasChanges = hasChanges;
			formState.isLoading = isLoading;
		}
	});

	onMount(async () => {
		// Initialize settings from loaded data
		if (data?.notificationSettings) {
			const discordSetting = data.notificationSettings.find((s) => s.provider === 'discord');
			if (discordSetting) {
				currentSettings.discordEnabled = discordSetting.enabled;
				currentSettings.discordWebhookUrl = discordSetting.config?.webhookUrl || '';
				currentSettings.discordUsername = discordSetting.config?.username || 'Arcane';
				currentSettings.discordAvatarUrl = discordSetting.config?.avatarUrl || '';
				currentSettings.discordEventImageUpdate = discordSetting.config?.events?.image_update ?? true;
				currentSettings.discordEventContainerUpdate = discordSetting.config?.events?.container_update ?? true;
			}

			const emailSetting = data.notificationSettings.find((s) => s.provider === 'email');
			if (emailSetting) {
				currentSettings.emailEnabled = emailSetting.enabled;
				currentSettings.emailSmtpHost = emailSetting.config?.smtpHost || '';
				currentSettings.emailSmtpPort = emailSetting.config?.smtpPort || 587;
				currentSettings.emailSmtpUsername = emailSetting.config?.smtpUsername || '';
				currentSettings.emailSmtpPassword = emailSetting.config?.smtpPassword || '';
				currentSettings.emailFromAddress = emailSetting.config?.fromAddress || '';
				currentSettings.emailToAddresses = (emailSetting.config?.toAddresses || []).join(', ');
				currentSettings.emailTlsMode = emailSetting.config?.tlsMode || 'starttls';
				currentSettings.emailEventImageUpdate = emailSetting.config?.events?.image_update ?? true;
				currentSettings.emailEventContainerUpdate = emailSetting.config?.events?.container_update ?? true;
			}

			// Sync form inputs after currentSettings is updated
			$formInputs.discordEnabled.value = currentSettings.discordEnabled;
			$formInputs.discordWebhookUrl.value = currentSettings.discordWebhookUrl;
			$formInputs.discordUsername.value = currentSettings.discordUsername;
			$formInputs.discordAvatarUrl.value = currentSettings.discordAvatarUrl;
			$formInputs.discordEventImageUpdate.value = currentSettings.discordEventImageUpdate;
			$formInputs.discordEventContainerUpdate.value = currentSettings.discordEventContainerUpdate;
			$formInputs.emailEnabled.value = currentSettings.emailEnabled;
			$formInputs.emailSmtpHost.value = currentSettings.emailSmtpHost;
			$formInputs.emailSmtpPort.value = currentSettings.emailSmtpPort;
			$formInputs.emailSmtpUsername.value = currentSettings.emailSmtpUsername;
			$formInputs.emailSmtpPassword.value = currentSettings.emailSmtpPassword;
			$formInputs.emailFromAddress.value = currentSettings.emailFromAddress;
			$formInputs.emailToAddresses.value = currentSettings.emailToAddresses;
			$formInputs.emailTlsMode.value = currentSettings.emailTlsMode;
			$formInputs.emailEventImageUpdate.value = currentSettings.emailEventImageUpdate;
			$formInputs.emailEventContainerUpdate.value = currentSettings.emailEventContainerUpdate;
		}

		// Load Apprise settings
		try {
			const settings = await notificationService.getAppriseSettings();
			appriseSettings = settings;
			savedAppriseSettings = { ...settings };
		} catch (error) {
			// Apprise not configured yet, keep defaults
		}

		if (formState) {
			formState.saveFunction = onSubmit;
			formState.resetFunction = resetForm;
		}
	});

	async function onSubmit() {
		const formData = form.validate();
		if (!formData) {
			toast.error('Please check the form for errors');
			return;
		}

		isLoading = true;

		try {
			const errors: string[] = [];

			// Save Discord settings
			try {
				await notificationService.updateSettings('discord', {
					provider: 'discord',
					enabled: formData.discordEnabled,
					config: {
						webhookUrl: formData.discordWebhookUrl,
						username: formData.discordUsername,
						avatarUrl: formData.discordAvatarUrl,
						events: {
							image_update: formData.discordEventImageUpdate,
							container_update: formData.discordEventContainerUpdate
						}
					}
				});
			} catch (error: any) {
				const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
				errors.push(m.notifications_saved_failed({ provider: 'Discord', error: errorMsg }));
			}

			// Save Email settings
			try {
				const toAddressArray = formData.emailToAddresses
					.split(',')
					.map((addr) => addr.trim())
					.filter((addr) => addr.length > 0);

				await notificationService.updateSettings('email', {
					provider: 'email',
					enabled: formData.emailEnabled,
					config: {
						smtpHost: formData.emailSmtpHost,
						smtpPort: formData.emailSmtpPort,
						smtpUsername: formData.emailSmtpUsername,
						smtpPassword: formData.emailSmtpPassword,
						fromAddress: formData.emailFromAddress,
						toAddresses: toAddressArray,
						tlsMode: formData.emailTlsMode,
						events: {
							image_update: formData.emailEventImageUpdate,
							container_update: formData.emailEventContainerUpdate
						}
					}
				});
			} catch (error: any) {
				const errorMsg = error?.response?.data?.error || error.message || 'Unknown error';
				errors.push(m.notifications_saved_failed({ provider: 'Email', error: errorMsg }));
			}

			// Save Apprise settings
			try {
				await notificationService.updateAppriseSettings(appriseSettings);
			} catch (error: any) {
				const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
				errors.push(m.notifications_saved_failed({ provider: 'Apprise', error: errorMsg }));
			}

			if (errors.length === 0) {
				currentSettings = formData;
				savedAppriseSettings = { ...appriseSettings };
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
		$formInputs.discordEnabled.value = currentSettings.discordEnabled;
		$formInputs.discordWebhookUrl.value = currentSettings.discordWebhookUrl;
		$formInputs.discordUsername.value = currentSettings.discordUsername;
		$formInputs.discordAvatarUrl.value = currentSettings.discordAvatarUrl;
		$formInputs.discordEventImageUpdate.value = currentSettings.discordEventImageUpdate;
		$formInputs.discordEventContainerUpdate.value = currentSettings.discordEventContainerUpdate;
		$formInputs.emailEnabled.value = currentSettings.emailEnabled;
		$formInputs.emailSmtpHost.value = currentSettings.emailSmtpHost;
		$formInputs.emailSmtpPort.value = currentSettings.emailSmtpPort;
		$formInputs.emailSmtpUsername.value = currentSettings.emailSmtpUsername;
		$formInputs.emailSmtpPassword.value = currentSettings.emailSmtpPassword;
		$formInputs.emailFromAddress.value = currentSettings.emailFromAddress;
		$formInputs.emailToAddresses.value = currentSettings.emailToAddresses;
		$formInputs.emailTlsMode.value = currentSettings.emailTlsMode;
		$formInputs.emailEventImageUpdate.value = currentSettings.emailEventImageUpdate;
		$formInputs.emailEventContainerUpdate.value = currentSettings.emailEventContainerUpdate;
		appriseSettings = { ...savedAppriseSettings };
	}

	async function testNotification(provider: 'discord' | 'email', testType: string = 'simple') {
		if (hasUnsavedChanges) {
			pendingTestAction = () => executeTest(provider, testType);
			showUnsavedDialog = true;
			return;
		}
		await executeTest(provider, testType);
	}

	async function executeTest(provider: 'discord' | 'email', testType: string = 'simple') {
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
		if (hasUnsavedChanges) {
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
	icon={BellIcon}
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

			<Tabs.Root value="built-in">
				<Tabs.List class="inline-flex w-auto">
					<Tabs.Trigger value="built-in">{m.notifications_tab_built_in()}</Tabs.Trigger>
					<Tabs.Trigger value="apprise">{m.notifications_tab_apprise()}</Tabs.Trigger>
				</Tabs.List>

				<Tabs.Content value="built-in" class="mt-4 space-y-4 sm:mt-6 sm:space-y-6">
					<Card.Root>
						<Card.Header icon={BellIcon}>
							<div class="flex flex-col space-y-1.5">
								<Card.Title>{m.notifications_discord_title()}</Card.Title>
								<Card.Description>{m.notifications_discord_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="space-y-4 px-3 py-4 sm:px-6">
							<SwitchWithLabel
								id="discord-enabled"
								bind:checked={$formInputs.discordEnabled.value}
								disabled={isReadOnly}
								label={m.notifications_discord_enabled_label()}
								description={m.notifications_discord_enabled_description()}
							/>

							{#if $formInputs.discordEnabled.value}
								<div class="space-y-4 border-l-2 pl-4">
									<TextInputWithLabel
										bind:value={$formInputs.discordWebhookUrl.value}
										disabled={isReadOnly}
										label={m.notifications_discord_webhook_url_label()}
										placeholder={m.notifications_discord_webhook_url_placeholder()}
										type="text"
										autocomplete="off"
										helpText={m.notifications_discord_webhook_url_help()}
									/>
									{#if $formInputs.discordWebhookUrl.error}
										<p class="text-destructive -mt-2 text-sm">{$formInputs.discordWebhookUrl.error}</p>
									{/if}

									<TextInputWithLabel
										bind:value={$formInputs.discordUsername.value}
										disabled={isReadOnly}
										label={m.notifications_discord_username_label()}
										placeholder={m.notifications_discord_username_placeholder()}
										type="text"
										autocomplete="off"
										helpText={m.notifications_discord_username_help()}
									/>

									<TextInputWithLabel
										bind:value={$formInputs.discordAvatarUrl.value}
										disabled={isReadOnly}
										label={m.notifications_discord_avatar_url_label()}
										placeholder={m.notifications_discord_avatar_url_placeholder()}
										type="text"
										autocomplete="off"
										helpText={m.notifications_discord_avatar_url_help()}
									/>

									<div class="space-y-3 pt-2">
										<Label class="text-sm font-medium">{m.notifications_events_title()}</Label>
										<p class="text-muted-foreground text-xs">{m.notifications_events_description()}</p>
										<div class="space-y-2">
											<SwitchWithLabel
												id="discord-event-image-update"
												bind:checked={$formInputs.discordEventImageUpdate.value}
												disabled={isReadOnly}
												label={m.notifications_event_image_update_label()}
												description={m.notifications_event_image_update_description()}
											/>
											<SwitchWithLabel
												id="discord-event-container-update"
												bind:checked={$formInputs.discordEventContainerUpdate.value}
												disabled={isReadOnly}
												label={m.notifications_event_container_update_label()}
												description={m.notifications_event_container_update_description()}
											/>
										</div>
									</div>
								</div>
							{/if}
						</Card.Content>
						<Card.Footer class="flex gap-2 px-3 py-4 sm:px-6">
							{#if $formInputs.discordEnabled.value}
								<Button variant="outline" onclick={() => testNotification('discord')} disabled={isReadOnly || isTesting}>
									{#if isTesting}
										<Spinner class="mr-2 h-4 w-4" />
									{:else}
										<SendIcon class="mr-2 h-4 w-4" />
									{/if}
									{m.notifications_discord_test_button()}
								</Button>
							{/if}
						</Card.Footer>
					</Card.Root>
					<Card.Root>
						<Card.Header icon={BellIcon}>
							<div class="flex flex-col space-y-1.5">
								<Card.Title>{m.notifications_email_title()}</Card.Title>
								<Card.Description>{m.notifications_email_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="space-y-4 px-3 py-4 sm:px-6">
							<SwitchWithLabel
								id="email-enabled"
								bind:checked={$formInputs.emailEnabled.value}
								disabled={isReadOnly}
								label={m.notifications_email_enabled_label()}
								description={m.notifications_email_enabled_description()}
							/>

							{#if $formInputs.emailEnabled.value}
								<div class="space-y-4 border-l-2 pl-4">
									<div class="grid grid-cols-2 gap-4">
										<TextInputWithLabel
											bind:value={$formInputs.emailSmtpHost.value}
											disabled={isReadOnly}
											label={m.notifications_email_smtp_host_label()}
											placeholder={m.notifications_email_smtp_host_placeholder()}
											type="text"
											autocomplete="off"
											helpText={m.notifications_email_smtp_host_help()}
										/>
										{#if $formInputs.emailSmtpHost.error}
											<p class="text-destructive col-span-2 -mt-2 text-sm">{$formInputs.emailSmtpHost.error}</p>
										{/if}

										<div class="space-y-2">
											<Label for="smtp-port">{m.notifications_email_smtp_port_label()}</Label>
											<Input
												id="smtp-port"
												type="number"
												bind:value={$formInputs.emailSmtpPort.value}
												disabled={isReadOnly}
												autocomplete="off"
												placeholder={m.notifications_email_smtp_port_placeholder()}
											/>
											<p class="text-muted-foreground text-sm">{m.notifications_email_smtp_port_help()}</p>
										</div>
									</div>

									<div class="grid grid-cols-2 gap-4">
										<TextInputWithLabel
											bind:value={$formInputs.emailSmtpUsername.value}
											disabled={isReadOnly}
											label={m.notifications_email_username_label()}
											placeholder={m.notifications_email_username_placeholder()}
											type="text"
											autocomplete="off"
											helpText={m.notifications_email_username_help()}
										/>

										<TextInputWithLabel
											bind:value={$formInputs.emailSmtpPassword.value}
											disabled={isReadOnly}
											label={m.notifications_email_password_label()}
											placeholder={m.notifications_email_password_placeholder()}
											type="password"
											autocomplete="new-password"
											helpText={m.notifications_email_password_help()}
										/>
									</div>

									<TextInputWithLabel
										bind:value={$formInputs.emailFromAddress.value}
										disabled={isReadOnly}
										label={m.notifications_email_from_address_label()}
										placeholder={m.notifications_email_from_address_placeholder()}
										type="email"
										autocomplete="off"
										helpText={m.notifications_email_from_address_help()}
									/>
									{#if $formInputs.emailFromAddress.error}
										<p class="text-destructive -mt-2 text-sm">{$formInputs.emailFromAddress.error}</p>
									{/if}

									<div class="space-y-2">
										<Label for="to-addresses">{m.notifications_email_to_addresses_label()}</Label>
										<Textarea
											id="to-addresses"
											bind:value={$formInputs.emailToAddresses.value}
											disabled={isReadOnly}
											autocomplete="off"
											placeholder={m.notifications_email_to_addresses_placeholder()}
											rows={2}
										/>
										{#if $formInputs.emailToAddresses.error}
											<p class="text-destructive text-sm">{$formInputs.emailToAddresses.error}</p>
										{:else}
											<p class="text-muted-foreground text-sm">{m.notifications_email_to_addresses_help()}</p>
										{/if}
									</div>

									<SelectWithLabel
										id="email-tls-mode"
										label={m.notifications_email_tls_mode_label()}
										bind:value={$formInputs.emailTlsMode.value}
										disabled={isReadOnly}
										placeholder={m.notifications_email_tls_mode_placeholder()}
										options={[
											{ value: 'none', label: 'None' },
											{ value: 'starttls', label: 'StartTLS' },
											{ value: 'ssl', label: 'SSL/TLS' }
										]}
										description={m.notifications_email_tls_mode_description()}
									/>
									<div class="space-y-3 pt-2">
										<Label class="text-sm font-medium">{m.notifications_events_title()}</Label>
										<p class="text-muted-foreground text-xs">{m.notifications_events_description()}</p>
										<div class="space-y-2">
											<SwitchWithLabel
												id="email-event-image-update"
												bind:checked={$formInputs.emailEventImageUpdate.value}
												disabled={isReadOnly}
												label={m.notifications_event_image_update_label()}
												description={m.notifications_event_image_update_description()}
											/>
											<SwitchWithLabel
												id="email-event-container-update"
												bind:checked={$formInputs.emailEventContainerUpdate.value}
												disabled={isReadOnly}
												label={m.notifications_event_container_update_label()}
												description={m.notifications_event_container_update_description()}
											/>
										</div>
									</div>
								</div>
							{/if}
						</Card.Content>
						<Card.Footer class="flex gap-2 px-3 py-4 sm:px-6">
							{#if $formInputs.emailEnabled.value}
								<DropdownMenu.Root>
									<DropdownMenu.Trigger>
										<Button variant="outline" disabled={isReadOnly || isTesting}>
											{#if isTesting}
												<Spinner class="mr-2 h-4 w-4" />
											{:else}
												<SendIcon class="mr-2 h-4 w-4" />
											{/if}
											{m.notifications_email_test_button()}
											<ChevronDownIcon class="ml-2 h-4 w-4" />
										</Button>
									</DropdownMenu.Trigger>
									<DropdownMenu.Content align="start">
										<DropdownMenu.Item onclick={() => testNotification('email', 'simple')}>
											<SendIcon class="mr-2 h-4 w-4" />
											{m.notifications_email_test_simple()}
										</DropdownMenu.Item>
										<DropdownMenu.Item onclick={() => testNotification('email', 'image-update')}>
											<SendIcon class="mr-2 h-4 w-4" />
											{m.notifications_email_test_image_update()}
										</DropdownMenu.Item>
									</DropdownMenu.Content>
								</DropdownMenu.Root>
							{/if}
						</Card.Footer>
					</Card.Root>
				</Tabs.Content>

				<Tabs.Content value="apprise" class="mt-4 space-y-4 sm:mt-6 sm:space-y-6">
					<Card.Root>
						<Card.Header icon={BellIcon}>
							<div class="flex flex-col space-y-1.5">
								<Card.Title>{m.notifications_apprise_title()}</Card.Title>
								<Card.Description>{m.notifications_apprise_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="space-y-4 px-3 py-4 sm:px-6">
							<SwitchWithLabel
								id="apprise-enabled"
								bind:checked={appriseSettings.enabled}
								disabled={isReadOnly}
								label={m.notifications_apprise_enabled_label()}
								description={m.notifications_apprise_enabled_description()}
							/>
							{#if appriseSettings.enabled}
								<div class="space-y-4 border-l-2 pl-4">
									<TextInputWithLabel
										bind:value={appriseSettings.apiUrl}
										disabled={isReadOnly}
										label={m.notifications_apprise_api_url_label()}
										placeholder={m.notifications_apprise_api_url_placeholder()}
										type="url"
										autocomplete="off"
										helpText={m.notifications_apprise_api_url_help()}
									/>
									<TextInputWithLabel
										bind:value={appriseSettings.imageUpdateTag}
										disabled={isReadOnly}
										label={m.notifications_apprise_image_tag_label()}
										placeholder={m.notifications_apprise_image_tag_placeholder()}
										type="text"
										autocomplete="off"
										helpText={m.notifications_apprise_image_tag_help()}
									/>
									<TextInputWithLabel
										bind:value={appriseSettings.containerUpdateTag}
										disabled={isReadOnly}
										label={m.notifications_apprise_container_tag_label()}
										placeholder={m.notifications_apprise_container_tag_placeholder()}
										type="text"
										autocomplete="off"
										helpText={m.notifications_apprise_container_tag_help()}
									/>
								</div>
							{/if}
						</Card.Content>
						<Card.Footer class="flex gap-2 px-3 py-4 sm:px-6">
							{#if appriseSettings.enabled}
								<Button variant="outline" onclick={() => testAppriseNotification()} disabled={isReadOnly || isTesting}>
									{#if isTesting}
										<Spinner class="mr-2 h-4 w-4" />
									{:else}
										<SendIcon class="mr-2 h-4 w-4" />
									{/if}
									{m.notifications_apprise_test_button()}
								</Button>
							{/if}
						</Card.Footer>
					</Card.Root>
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
			<Button variant="outline" onclick={() => (showUnsavedDialog = false)}>
				{m.common_cancel()}
			</Button>
			<Button onclick={handleSaveAndTest}>
				{m.notifications_unsaved_changes_save_and_test()}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
