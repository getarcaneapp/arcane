<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Label } from '$lib/components/ui/label';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { TelegramFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: TelegramFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			botToken: z.string(),
			chatIds: z.string(),
			preview: z.boolean(),
			notification: z.boolean(),
			title: z.string(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.botToken.trim()) {
				ctx.addIssue({ code: 'custom', message: 'Bot Token is required when Telegram is enabled', path: ['botToken'] });
			}
			if (!d.chatIds.trim()) {
				ctx.addIssue({ code: 'custom', message: 'At least one Chat ID is required when Telegram is enabled', path: ['chatIds'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof TelegramFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof TelegramFormValues | undefined;
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
	id="telegram"
	title="Telegram"
	description="Send notifications via Telegram bot"
	enabledLabel="Enable Telegram Notifications"
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.botToken}
		{disabled}
		label="Bot Token"
		placeholder="123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
		type="password"
		autocomplete="off"
		helpText="The bot token from @BotFather"
		error={fieldErrors.botToken}
	/>

	<div class="space-y-2">
		<Label for="telegram-chat-ids">Chat IDs</Label>
		<Textarea
			id="telegram-chat-ids"
			bind:value={values.chatIds}
			{disabled}
			autocomplete="off"
			placeholder="@channel, 123456789, @another_channel"
			rows={2}
		/>
		{#if fieldErrors.chatIds}
			<p class="text-destructive text-sm">{fieldErrors.chatIds}</p>
		{:else}
			<p class="text-muted-foreground text-sm">Comma-separated list of chat IDs or @channel names</p>
		{/if}
	</div>

	<TextInputWithLabel
		bind:value={values.title}
		{disabled}
		label="Title (Optional)"
		placeholder="Arcane Notifications"
		type="text"
		autocomplete="off"
		helpText="Custom title for notifications"
	/>

	<div class="space-y-3">
		<SwitchWithLabel
			id="telegram-preview"
			bind:checked={values.preview}
			{disabled}
			label="Enable Link Previews"
			description="Show web page previews for URLs in messages"
		/>
		<SwitchWithLabel
			id="telegram-notification"
			bind:checked={values.notification}
			{disabled}
			label="Enable Notification Sound"
			description="Play notification sound when messages are received"
		/>
	</div>

	<EventSubscriptions
		providerId="telegram"
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
