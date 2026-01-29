<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { SlackFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: SlackFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			token: z.string(),
			botName: z.string(),
			icon: z.string(),
			color: z.string(),
			title: z.string(),
			channel: z.string(),
			threadTs: z.string(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;

			// Token is required when enabled
			if (!d.token.trim()) {
				ctx.addIssue({ code: 'custom', message: m.notifications_slack_token_required(), path: ['token'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof SlackFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof SlackFormValues | undefined;
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
	id="slack"
	title={m.notifications_slack_title()}
	description={m.notifications_slack_description()}
	enabledLabel={m.notifications_slack_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.token}
		{disabled}
		label={m.notifications_slack_token_label()}
		placeholder={m.notifications_slack_token_placeholder()}
		type="password"
		autocomplete="off"
		helpText={m.notifications_slack_token_help()}
		error={fieldErrors.token}
	/>

	<div class="grid grid-cols-2 gap-4">
		<TextInputWithLabel
			bind:value={values.botName}
			{disabled}
			label={m.notifications_slack_bot_name_label()}
			placeholder={m.notifications_slack_bot_name_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_slack_bot_name_help()}
		/>

		<TextInputWithLabel
			bind:value={values.channel}
			{disabled}
			label={m.notifications_slack_channel_label()}
			placeholder={m.notifications_slack_channel_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_slack_channel_help()}
		/>
	</div>

	<div class="grid grid-cols-2 gap-4">
		<TextInputWithLabel
			bind:value={values.icon}
			{disabled}
			label={m.notifications_slack_icon_label()}
			placeholder={m.notifications_slack_icon_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_slack_icon_help()}
		/>

		<TextInputWithLabel
			bind:value={values.color}
			{disabled}
			label={m.notifications_slack_color_label()}
			placeholder={m.notifications_slack_color_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_slack_color_help()}
		/>
	</div>

	<div class="grid grid-cols-2 gap-4">
		<TextInputWithLabel
			bind:value={values.title}
			{disabled}
			label={m.notifications_slack_title_label()}
			placeholder={m.notifications_slack_title_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_slack_title_help()}
		/>

		<TextInputWithLabel
			bind:value={values.threadTs}
			{disabled}
			label={m.notifications_slack_thread_ts_label()}
			placeholder={m.notifications_slack_thread_ts_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_slack_thread_ts_help()}
		/>
	</div>

	<EventSubscriptions
		providerId="slack"
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
