<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { Label } from '$lib/components/ui/label';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import { m } from '$lib/paraglide/messages';
	import { ArrowDownIcon, SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { EmailFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: EmailFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: (testType: string) => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			smtpHost: z.string(),
			smtpPort: z.coerce.number().int().min(1).max(65535),
			smtpUsername: z.string(),
			smtpPassword: z.string(),
			fromAddress: z.string(),
			toAddresses: z.string(),
			tlsMode: z.enum(['none', 'starttls', 'ssl']),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.smtpHost.trim()) {
				ctx.addIssue({ code: 'custom', message: 'SMTP host is required when email is enabled', path: ['smtpHost'] });
			}
			if (!d.fromAddress.trim()) {
				ctx.addIssue({ code: 'custom', message: 'From address is required when email is enabled', path: ['fromAddress'] });
			} else {
				const v = z.string().email().safeParse(d.fromAddress.trim());
				if (!v.success) {
					ctx.addIssue({ code: 'custom', message: 'Invalid email address format', path: ['fromAddress'] });
				}
			}
			if (!d.toAddresses.trim()) {
				ctx.addIssue({
					code: 'custom',
					message: 'At least one recipient address is required when email is enabled',
					path: ['toAddresses']
				});
			} else {
				const addresses = d.toAddresses
					.split(',')
					.map((addr) => addr.trim())
					.filter((addr) => addr.length > 0);
				const invalid: string[] = [];
				addresses.forEach((addr) => {
					const v = z.string().email().safeParse(addr);
					if (!v.success) invalid.push(addr);
				});
				if (invalid.length > 0) {
					ctx.addIssue({
						code: 'custom',
						message: `Invalid email addresses: ${invalid.join(', ')}`,
						path: ['toAddresses']
					});
				}
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof EmailFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof EmailFormValues | undefined;
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
	id="email"
	title="Email"
	description={m.notifications_email_description()}
	enabledLabel={m.notifications_email_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<div class="grid grid-cols-2 gap-4">
		<TextInputWithLabel
			bind:value={values.smtpHost}
			{disabled}
			label={m.notifications_email_smtp_host_label()}
			placeholder={m.notifications_email_smtp_host_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_email_smtp_host_help()}
			error={fieldErrors.smtpHost}
		/>

		<div class="space-y-2">
			<Label for="smtp-port">{m.notifications_email_smtp_port_label()}</Label>
			<input
				id="smtp-port"
				type="number"
				class="border-input file:text-foreground placeholder:text-muted-foreground focus-visible:ring-ring flex h-9 w-full rounded-md border bg-transparent px-3 py-1 text-base shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium focus-visible:ring-1 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"
				bind:value={values.smtpPort}
				{disabled}
				autocomplete="off"
				placeholder={m.notifications_email_smtp_port_placeholder()}
			/>
			<p class="text-muted-foreground text-sm">{m.notifications_email_smtp_port_help()}</p>
		</div>
	</div>

	<div class="grid grid-cols-2 gap-4">
		<TextInputWithLabel
			bind:value={values.smtpUsername}
			{disabled}
			label={m.notifications_email_username_label()}
			placeholder={m.notifications_email_username_placeholder()}
			type="text"
			autocomplete="off"
			helpText={m.notifications_email_username_help()}
		/>

		<TextInputWithLabel
			bind:value={values.smtpPassword}
			{disabled}
			label={m.notifications_email_password_label()}
			placeholder={m.notifications_email_password_placeholder()}
			type="password"
			autocomplete="new-password"
			helpText={m.notifications_email_password_help()}
		/>
	</div>

	<TextInputWithLabel
		bind:value={values.fromAddress}
		{disabled}
		label={m.notifications_email_from_address_label()}
		placeholder={m.notifications_email_from_address_placeholder()}
		type="email"
		autocomplete="off"
		helpText={m.notifications_email_from_address_help()}
		error={fieldErrors.fromAddress}
	/>

	<div class="space-y-2">
		<Label for="to-addresses">{m.notifications_email_to_addresses_label()}</Label>
		<Textarea
			id="to-addresses"
			bind:value={values.toAddresses}
			{disabled}
			autocomplete="off"
			placeholder={m.notifications_email_to_addresses_placeholder()}
			rows={2}
		/>
		{#if fieldErrors.toAddresses}
			<p class="text-destructive text-sm">{fieldErrors.toAddresses}</p>
		{:else}
			<p class="text-muted-foreground text-sm">{m.notifications_email_to_addresses_help()}</p>
		{/if}
	</div>

	<SelectWithLabel
		id="email-tls-mode"
		label={m.notifications_email_tls_mode_label()}
		bind:value={values.tlsMode}
		{disabled}
		placeholder={m.notifications_email_tls_mode_placeholder()}
		options={[
			{ value: 'none', label: 'None' },
			{ value: 'starttls', label: 'StartTLS' },
			{ value: 'ssl', label: 'SSL/TLS' }
		]}
		description={m.notifications_email_tls_mode_description()}
	/>

	<EventSubscriptions
		providerId="email"
		bind:eventImageUpdate={values.eventImageUpdate}
		bind:eventContainerUpdate={values.eventContainerUpdate}
		{disabled}
	/>

	{#if onTest}
		<div class="pt-2">
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					<ArcaneButton
						action="base"
						tone="outline"
						disabled={disabled || isTesting}
						loading={isTesting}
						icon={SendEmailIcon}
						customLabel={m.notifications_test_notification()}
					>
						<ArrowDownIcon class="ml-2 size-4" />
					</ArcaneButton>
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="start">
					<DropdownMenu.Item onclick={() => onTest('simple')}>
						<SendEmailIcon class="size-4" />
						{m.notifications_email_test_simple()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => onTest('image-update')}>
						<SendEmailIcon class="size-4" />
						{m.notifications_email_test_image_update()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => onTest('batch-image-update')}>
						<SendEmailIcon class="size-4" />
						{m.notifications_email_test_batch_image_update()}
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	{/if}
</ProviderFormWrapper>
