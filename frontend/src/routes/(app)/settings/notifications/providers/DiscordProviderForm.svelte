<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { DiscordFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: DiscordFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			webhookId: z.string(),
			token: z.string(),
			username: z.string(),
			avatarUrl: z.string(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.webhookId.trim()) {
				ctx.addIssue({ code: 'custom', message: 'Webhook ID is required when Discord is enabled', path: ['webhookId'] });
			}
			if (!d.token.trim()) {
				ctx.addIssue({ code: 'custom', message: 'Webhook Token is required when Discord is enabled', path: ['token'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof DiscordFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof DiscordFormValues | undefined;
			if (!key || errs[key]) continue;
			errs[key] = issue.message;
		}
		return errs;
	});

	export function isValid(): boolean {
		return validation.success;
	}
</script>

<ProviderFormWrapper
	id="discord"
	title="Discord"
	description={m.notifications_discord_description()}
	enabledLabel={m.notifications_discord_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.webhookId}
		{disabled}
		label={m.notifications_discord_webhook_id_label()}
		placeholder={m.notifications_discord_webhook_id_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_discord_webhook_id_help()}
		error={fieldErrors.webhookId}
	/>

	<TextInputWithLabel
		bind:value={values.token}
		{disabled}
		label={m.notifications_discord_token_label()}
		placeholder={m.notifications_discord_token_placeholder()}
		type="password"
		autocomplete="off"
		helpText={m.notifications_discord_token_help()}
		error={fieldErrors.token}
	/>

	<TextInputWithLabel
		bind:value={values.username}
		{disabled}
		label={m.notifications_discord_username_label()}
		placeholder={m.notifications_discord_username_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_discord_username_help()}
	/>

	<TextInputWithLabel
		bind:value={values.avatarUrl}
		{disabled}
		label={m.notifications_discord_avatar_url_label()}
		placeholder={m.notifications_discord_avatar_url_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_discord_avatar_url_help()}
	/>

	<EventSubscriptions
		providerId="discord"
		bind:eventImageUpdate={values.eventImageUpdate}
		bind:eventContainerUpdate={values.eventContainerUpdate}
		{disabled}
	/>

	{#if onTest}
		<div class="pt-2">
			<ArcaneButton
				action="base"
				tone="outline"
				onclick={onTest}
				disabled={disabled || isTesting}
				loading={isTesting}
				icon={SendEmailIcon}
				customLabel={m.notifications_test_notification()}
			/>
		</div>
	{/if}
</ProviderFormWrapper>
