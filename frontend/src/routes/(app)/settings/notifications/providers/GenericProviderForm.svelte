<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { GenericFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: GenericFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			webhookUrl: z.string(),
			method: z.string(),
			contentType: z.string(),
			titleKey: z.string(),
			messageKey: z.string(),
			customHeaders: z.string(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.webhookUrl.trim()) {
				ctx.addIssue({
					code: 'custom',
					message: 'Webhook URL is required when Generic Webhook is enabled',
					path: ['webhookUrl']
				});
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof GenericFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof GenericFormValues | undefined;
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
	id="generic"
	title={m.notifications_generic_title()}
	description={m.notifications_generic_description()}
	enabledLabel={m.notifications_generic_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.webhookUrl}
		{disabled}
		label={m.notifications_generic_webhook_url_label()}
		placeholder={m.notifications_generic_webhook_url_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_generic_webhook_url_help()}
	/>
	{#if fieldErrors.webhookUrl}
		<p class="text-destructive -mt-2 text-sm">{fieldErrors.webhookUrl}</p>
	{/if}

	<TextInputWithLabel
		bind:value={values.method}
		{disabled}
		label={m.notifications_generic_method_label()}
		placeholder={m.notifications_generic_method_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_generic_method_help()}
	/>

	<TextInputWithLabel
		bind:value={values.contentType}
		{disabled}
		label={m.notifications_generic_content_type_label()}
		placeholder={m.notifications_generic_content_type_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_generic_content_type_help()}
	/>

	<TextInputWithLabel
		bind:value={values.titleKey}
		{disabled}
		label={m.notifications_generic_title_key_label()}
		placeholder={m.notifications_generic_title_key_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_generic_title_key_help()}
	/>

	<TextInputWithLabel
		bind:value={values.messageKey}
		{disabled}
		label={m.notifications_generic_message_key_label()}
		placeholder={m.notifications_generic_message_key_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_generic_message_key_help()}
	/>

	<TextInputWithLabel
		bind:value={values.customHeaders}
		{disabled}
		label={m.notifications_generic_custom_headers_label()}
		placeholder={m.notifications_generic_custom_headers_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_generic_custom_headers_help()}
	/>

	<EventSubscriptions
		providerId="generic"
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
