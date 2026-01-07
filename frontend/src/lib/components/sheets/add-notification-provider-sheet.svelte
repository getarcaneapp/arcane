<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import type { NotificationSettings } from '$lib/types/notification.type';
	import { notificationProviders } from '$lib/types/notification.type';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, SaveIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';

	type ProviderFormProps = {
		open: boolean;
		providerToEdit: NotificationSettings | null;
		onSubmit: (data: NotificationSettings) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), providerToEdit = $bindable(), onSubmit, isLoading }: ProviderFormProps = $props();

	let isEditMode = $derived(!!providerToEdit);
	let SubmitIcon = $derived(isEditMode ? SaveIcon : AddIcon);
	let provider = $state(providerToEdit?.provider || 'discord');

	const formSchema = z.object({
		name: z.string().min(1, 'Name is required'),
		enabled: z.boolean().default(true),
		webhookUrl: z.string().default(''),
		fromAddress: z.string().default(''),
		toAddresses: z.string().default(''),
		smtpHost: z.string().default(''),
		smtpPort: z.number().default(587),
		smtpUsername: z.string().default(''),
		smtpPassword: z.string().default(''),
		tlsMode: z.string().default('starttls'),
		skipTLSVerify: z.boolean().default(false),
		url: z.string().default('')
	});

	let formData = $derived({
		name: providerToEdit?.name || '',
		enabled: providerToEdit?.enabled ?? true,
		webhookUrl: (providerToEdit?.config?.webhookUrl as string) || '',
		fromAddress: (providerToEdit?.config?.fromAddress as string) || '',
		toAddresses: (providerToEdit?.config?.toAddresses as string) || '',
		smtpHost: (providerToEdit?.config?.smtpHost as string) || '',
		smtpPort: (providerToEdit?.config?.smtpPort as number) || 587,
		smtpUsername: (providerToEdit?.config?.smtpUsername as string) || '',
		smtpPassword: (providerToEdit?.config?.smtpPassword as string) || '',
		tlsMode: (providerToEdit?.config?.tlsMode as string) || 'starttls',
		skipTLSVerify: (providerToEdit?.config?.skipTLSVerify as boolean) ?? false,
		url: (providerToEdit?.config?.url as string) || ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function isValidEmail(email: string): boolean {
		const trimmed = email.trim();
		return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed);
	}

	function validateEmailAddresses(addresses: string): string | null {
		if (!addresses.trim()) return 'Recipient addresses are required';
		const emails = addresses.split(',').map((e) => e.trim());
		const invalidEmails = emails.filter((e) => e && !isValidEmail(e));
		if (invalidEmails.length > 0) {
			return `Invalid email format: ${invalidEmails.join(', ')}`;
		}
		return null;
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		// Manual validation based on provider
		if (provider === 'email') {
			if (!data.fromAddress) {
				$inputs.fromAddress.error = 'Sender address is required';
				return;
			}
			const toAddressError = validateEmailAddresses(data.toAddresses);
			if (toAddressError) {
				$inputs.toAddresses.error = toAddressError;
				return;
			}
			if (!data.smtpHost) {
				$inputs.smtpHost.error = 'SMTP host is required';
				return;
			}
		} else if ((provider === 'discord' || provider === 'webhook' || provider === 'slack') && !data.webhookUrl) {
			$inputs.webhookUrl.error = 'Webhook URL is required';
			return;
		}

		const { name, enabled, ...allConfig } = data;

		// Filter config to only include relevant fields
		const config: Record<string, any> = {};
		if (provider === 'email') {
			config.fromAddress = allConfig.fromAddress;
			config.toAddresses = allConfig.toAddresses;
			config.smtpHost = allConfig.smtpHost;
			config.smtpPort = allConfig.smtpPort;
			config.smtpUsername = allConfig.smtpUsername;
			config.smtpPassword = allConfig.smtpPassword;
			config.tlsMode = allConfig.tlsMode;
			config.skipTLSVerify = allConfig.skipTLSVerify;
		} else if (provider === 'discord' || provider === 'webhook' || provider === 'slack') {
			config.webhookUrl = allConfig.webhookUrl;
		} else {
			config.url = allConfig.url;
		}

		const providerData: NotificationSettings = {
			id: providerToEdit?.id || '',
			name,
			provider,
			enabled,
			config
		};

		onSubmit(providerData);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			providerToEdit = null;
			provider = 'discord';
		}
	}

	const providerOptions = $derived(
		Object.entries(notificationProviders).map(([value, labelFn]) => ({
			value,
			label: labelFn()
		}))
	);
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.common_edit() : m.common_add_button({ resource: 'Provider' })}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput label={m.common_name()} placeholder={m.common_name()} bind:input={$inputs.name} />

			<SelectWithLabel id="provider-select" label={m.common_type()} bind:value={provider} options={providerOptions} />

			{#if provider === 'email'}
				<div class="space-y-4 rounded-lg border p-4">
					<h4 class="text-muted-foreground text-sm font-medium">Configuration</h4>

					<FormInput
						label={m.notification_field_from_address()}
						placeholder="arcane@example.com"
						bind:input={$inputs.fromAddress}
					/>
					<FormInput
						label={m.notification_field_to_addresses()}
						helpText="Comma separated list of email addresses"
						bind:input={$inputs.toAddresses}
					/>
					<FormInput label={m.notification_field_smtp_host()} placeholder="smtp.gmail.com" bind:input={$inputs.smtpHost} />
					<FormInput label={m.notification_field_smtp_port()} type="number" bind:input={$inputs.smtpPort} />
					<FormInput label={m.notification_field_smtp_username()} bind:input={$inputs.smtpUsername} />
					<FormInput label={m.notification_field_smtp_password()} type="password" bind:input={$inputs.smtpPassword} />
					<SelectWithLabel
						id="tlsMode"
						label={m.notification_field_tls_mode()}
						bind:value={$inputs.tlsMode.value}
						error={$inputs.tlsMode.error}
						options={[
							{ value: 'none', label: m.notification_field_tls_mode_none() },
							{ value: 'starttls', label: m.notification_field_tls_mode_starttls() },
							{ value: 'ssl', label: m.notification_field_tls_mode_ssl() }
						]}
					/>
					<FormInput label={m.notification_field_skip_tls_verify()} type="switch" bind:input={$inputs.skipTLSVerify} />
				</div>
			{:else if provider === 'discord'}
				<div class="space-y-4 rounded-lg border p-4">
					<h4 class="text-muted-foreground text-sm font-medium">Configuration</h4>
					<FormInput
						label={m.notification_field_webhook_url()}
						placeholder="https://discord.com/api/webhooks/..."
						bind:input={$inputs.webhookUrl}
					/>
				</div>
			{:else}
				<div class="space-y-4 rounded-lg border p-4">
					<h4 class="text-muted-foreground text-sm font-medium">Configuration</h4>
					<FormInput label={m.notification_field_server_url()} helpText="Full Shoutrrr URL" bind:input={$inputs.url} />
				</div>
			{/if}

			<FormInput label={m.common_enabled()} type="switch" bind:input={$inputs.enabled} />
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton
				action="cancel"
				tone="outline"
				type="button"
				class="flex-1"
				onclick={() => (open = false)}
				disabled={isLoading}
			/>
			<ArcaneButton
				action={isEditMode ? 'save' : 'create'}
				type="submit"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={isEditMode ? m.common_save() : m.common_add_button({ resource: 'Provider' })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
