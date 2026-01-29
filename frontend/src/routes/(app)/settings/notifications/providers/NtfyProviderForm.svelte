<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Label } from '$lib/components/ui/label';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { NtfyFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: NtfyFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			host: z.string(),
			port: z.number().min(0).max(65535),
			topic: z.string(),
			username: z.string(),
			password: z.string(),
			priority: z.string(),
			tags: z.string(),
			icon: z.string(),
			cache: z.boolean(),
			firebase: z.boolean(),
			disableTlsVerification: z.boolean(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.topic.trim()) {
				ctx.addIssue({ code: 'custom', message: 'Topic is required when Ntfy is enabled', path: ['topic'] });
			}
			if (d.port > 0 && (d.port < 1 || d.port > 65535)) {
				ctx.addIssue({ code: 'custom', message: 'Port must be between 1 and 65535', path: ['port'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof NtfyFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof NtfyFormValues | undefined;
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
	id="ntfy"
	title="Ntfy"
	description={m.notifications_ntfy_description()}
	enabledLabel={m.notifications_ntfy_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.host}
		{disabled}
		label={m.notifications_ntfy_host_label()}
		placeholder={m.notifications_ntfy_host_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_ntfy_host_help()}
	/>

	<TextInputWithLabel
		bind:value={values.port}
		{disabled}
		label={m.notifications_ntfy_port_label()}
		placeholder={m.notifications_ntfy_port_placeholder()}
		type="number"
		autocomplete="off"
		helpText={m.notifications_ntfy_port_help()}
	/>

	<TextInputWithLabel
		bind:value={values.topic}
		{disabled}
		label={m.notifications_ntfy_topic_label()}
		placeholder={m.notifications_ntfy_topic_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_ntfy_topic_help()}
		error={fieldErrors.topic}
	/>

	<TextInputWithLabel
		bind:value={values.username}
		{disabled}
		label={m.notifications_ntfy_username_label()}
		placeholder={m.notifications_ntfy_username_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_ntfy_username_help()}
	/>

	<TextInputWithLabel
		bind:value={values.password}
		{disabled}
		label={m.notifications_ntfy_password_label()}
		placeholder={m.notifications_ntfy_password_placeholder()}
		type="password"
		autocomplete="off"
		helpText={m.notifications_ntfy_password_help()}
	/>

	<div class="space-y-2">
		<Label for="ntfy-priority">{m.notifications_ntfy_priority_label()}</Label>
		<select
			id="ntfy-priority"
			bind:value={values.priority}
			{disabled}
			class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 rounded-md border px-3 py-2 text-base focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"
		>
			<option value="min">Min (1)</option>
			<option value="low">Low (2)</option>
			<option value="default">Default (3)</option>
			<option value="high">High (4)</option>
			<option value="max">Max/Urgent (5)</option>
		</select>
		<p class="text-muted-foreground text-sm">{m.notifications_ntfy_priority_help()}</p>
	</div>

	<div class="space-y-2">
		<Label for="ntfy-tags">{m.notifications_ntfy_tags_label()}</Label>
		<Textarea
			id="ntfy-tags"
			bind:value={values.tags}
			{disabled}
			autocomplete="off"
			placeholder={m.notifications_ntfy_tags_placeholder()}
			rows={2}
		/>
		<p class="text-muted-foreground text-sm">{m.notifications_ntfy_tags_help()}</p>
	</div>

	<TextInputWithLabel
		bind:value={values.icon}
		{disabled}
		label={m.notifications_ntfy_icon_label()}
		placeholder={m.notifications_ntfy_icon_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_ntfy_icon_help()}
	/>

	<div class="space-y-3">
		<SwitchWithLabel
			id="ntfy-cache"
			bind:checked={values.cache}
			{disabled}
			label={m.notifications_ntfy_cache_label()}
			description={m.notifications_ntfy_cache_help()}
		/>
		<SwitchWithLabel
			id="ntfy-firebase"
			bind:checked={values.firebase}
			{disabled}
			label={m.notifications_ntfy_firebase_label()}
			description={m.notifications_ntfy_firebase_help()}
		/>
		<SwitchWithLabel
			id="ntfy-disable-tls"
			bind:checked={values.disableTlsVerification}
			{disabled}
			label={m.notifications_ntfy_disable_tls_label()}
			description={m.notifications_ntfy_disable_tls_help()}
		/>
	</div>

	<EventSubscriptions
		providerId="ntfy"
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
