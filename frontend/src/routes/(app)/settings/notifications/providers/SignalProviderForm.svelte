<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Label } from '$lib/components/ui/label';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { SignalFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: SignalFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			host: z.string(),
			port: z.number().min(1).max(65535),
			user: z.string(),
			password: z.string(),
			token: z.string(),
			source: z.string(),
			recipients: z.string(),
			disableTls: z.boolean(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;

			// Host is required when enabled
			if (!d.host.trim()) {
				ctx.addIssue({ code: 'custom', message: m.notifications_signal_host_required(), path: ['host'] });
			}

			// Port validation
			if (d.port < 1 || d.port > 65535) {
				ctx.addIssue({ code: 'custom', message: m.notifications_signal_port_invalid(), path: ['port'] });
			}

			// Source phone number is required
			if (!d.source.trim()) {
				ctx.addIssue({ code: 'custom', message: m.notifications_signal_source_required(), path: ['source'] });
			} else if (!d.source.startsWith('+')) {
				ctx.addIssue({ code: 'custom', message: m.notifications_signal_source_format(), path: ['source'] });
			}

			// Recipients are required
			if (!d.recipients.trim()) {
				ctx.addIssue({ code: 'custom', message: m.notifications_signal_recipients_required(), path: ['recipients'] });
			}

			// Either user+password or token is required
			const hasBasicAuth = d.user.trim() && d.password.trim();
			const hasTokenAuth = d.token.trim();
			if (!hasBasicAuth && !hasTokenAuth) {
				ctx.addIssue({
					code: 'custom',
					message: m.notifications_signal_auth_required(),
					path: ['user']
				});
			}
			if (hasBasicAuth && hasTokenAuth) {
				ctx.addIssue({
					code: 'custom',
					message: m.notifications_signal_auth_conflict(),
					path: ['token']
				});
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof SignalFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof SignalFormValues | undefined;
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
	id="signal"
	title={m.notifications_signal_title()}
	description={m.notifications_signal_description()}
	enabledLabel={m.notifications_signal_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<div class="grid grid-cols-2 gap-4">
		<TextInputWithLabel
			bind:value={values.host}
			{disabled}
			label={m.notifications_signal_host_label()}
			placeholder={m.notifications_signal_host_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_signal_host_help()}
			error={fieldErrors.host}
		/>

		<TextInputWithLabel
			bind:value={values.port}
			{disabled}
			label={m.notifications_signal_port_label()}
			placeholder={m.notifications_signal_port_placeholder()}
			type="number"
			autocomplete="off"
			helpText={m.notifications_signal_port_help()}
			error={fieldErrors.port}
		/>
	</div>

	<div class="space-y-4">
		<div class="rounded-lg border p-4">
			<h4 class="mb-3 text-sm font-medium">{m.notifications_signal_auth_section()}</h4>
			<p class="text-muted-foreground mb-4 text-sm">{m.notifications_signal_auth_help()}</p>

			<div class="space-y-4">
				<TextInputWithLabel
					bind:value={values.user}
					{disabled}
					label={m.notifications_signal_user_label()}
					placeholder={m.notifications_signal_user_placeholder()}
					type="text"
					autocomplete="off"
					helpText={m.notifications_signal_user_help()}
				/>

				<TextInputWithLabel
					bind:value={values.password}
					{disabled}
					label={m.notifications_signal_password_label()}
					placeholder={m.notifications_signal_password_placeholder()}
					type="password"
					autocomplete="off"
					helpText={m.notifications_signal_password_help()}
				/>

				<div class="relative">
					<div class="absolute inset-0 flex items-center">
						<span class="w-full border-t"></span>
					</div>
					<div class="relative flex justify-center text-xs uppercase">
						<span class="bg-background text-muted-foreground px-2">{m.notifications_signal_auth_or()}</span>
					</div>
				</div>

				<TextInputWithLabel
					bind:value={values.token}
					{disabled}
					label={m.notifications_signal_token_label()}
					placeholder={m.notifications_signal_token_placeholder()}
					type="password"
					autocomplete="off"
					helpText={m.notifications_signal_token_help()}
				/>
			</div>

			{#if fieldErrors.user || fieldErrors.token}
				<p class="text-destructive mt-2 text-sm">{fieldErrors.user || fieldErrors.token}</p>
			{/if}
		</div>
	</div>

	<TextInputWithLabel
		bind:value={values.source}
		{disabled}
		label={m.notifications_signal_source_label()}
		placeholder={m.notifications_signal_source_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_signal_source_help()}
		error={fieldErrors.source}
	/>

	<div class="space-y-2">
		<Label for="signal-recipients">{m.notifications_signal_recipients_label()}</Label>
		<Textarea
			id="signal-recipients"
			bind:value={values.recipients}
			{disabled}
			autocomplete="off"
			placeholder={m.notifications_signal_recipients_placeholder()}
			rows={3}
		/>
		{#if fieldErrors.recipients}
			<p class="text-destructive text-sm">{fieldErrors.recipients}</p>
		{:else}
			<p class="text-muted-foreground text-sm">{m.notifications_signal_recipients_help()}</p>
		{/if}
	</div>

	<SwitchWithLabel
		id="signal-disable-tls"
		bind:checked={values.disableTls}
		{disabled}
		label={m.notifications_signal_disable_tls_label()}
		description={m.notifications_signal_disable_tls_description()}
	/>

	<EventSubscriptions
		providerId="signal"
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
