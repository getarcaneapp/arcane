<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Label } from '$lib/components/ui/label';
	import * as Select from '$lib/components/ui/select/index.js';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { GotifyFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: GotifyFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const priorityOptions = [
		{ value: '-2', label: '-2 (Min)' },
		{ value: '-1', label: '-1 (Low)' },
		{ value: '0', label: '0 (None)' },
		{ value: '1', label: '1 (Low)' },
		{ value: '2', label: '2' },
		{ value: '3', label: '3' },
		{ value: '4', label: '4 (Normal)' },
		{ value: '5', label: '5' },
		{ value: '6', label: '6' },
		{ value: '7', label: '7 (High)' },
		{ value: '8', label: '8' },
		{ value: '9', label: '9' },
		{ value: '10', label: '10 (Max)' }
	];

	const priorityValue = $derived(String(values.priority ?? 0));

	const schema = z
		.object({
			enabled: z.boolean(),
			host: z.string(),
			port: z.coerce.number().int().min(0).max(65535),
			token: z.string(),
			path: z.string(),
			priority: z.coerce.number().int(),
			title: z.string(),
			disableTls: z.boolean(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.host.trim()) {
				ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['host'] });
			}
			if (!d.token.trim()) {
				ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['token'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const selectedPriorityLabel = $derived(
		priorityOptions.find((option) => option.value === priorityValue)?.label ?? priorityValue
	);

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof GotifyFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof GotifyFormValues | undefined;
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
	id="gotify"
	title="Gotify"
	description={m.notifications_gotify_description()}
	enabledLabel={m.notifications_gotify_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<div class="grid grid-cols-1 gap-4 md:grid-cols-4">
		<div class="md:col-span-3">
			<TextInputWithLabel
				bind:value={values.host}
				{disabled}
				label={m.notifications_gotify_host_label()}
				placeholder={m.notifications_gotify_host_placeholder()}
				type="text"
				autocomplete="off"
				helpText={m.notifications_gotify_host_help()}
				error={fieldErrors.host}
			/>
		</div>
		<div class="md:col-span-1">
			<TextInputWithLabel
				bind:value={values.port}
				{disabled}
				label={m.notifications_gotify_port_label()}
				placeholder={m.notifications_gotify_port_placeholder()}
				type="number"
				autocomplete="off"
				helpText={m.notifications_gotify_port_help()}
				error={fieldErrors.port}
			/>
		</div>
	</div>

	<TextInputWithLabel
		bind:value={values.token}
		{disabled}
		label={m.notifications_gotify_token_label()}
		placeholder={m.notifications_gotify_token_placeholder()}
		type="password"
		autocomplete="off"
		helpText={m.notifications_gotify_token_help()}
		error={fieldErrors.token}
	/>

	<TextInputWithLabel
		bind:value={values.path}
		{disabled}
		label={m.notifications_gotify_path_label()}
		placeholder={m.notifications_gotify_path_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_gotify_path_help()}
		error={fieldErrors.path}
	/>

	<div class="space-y-2">
		<Label for="gotify-priority">{m.notifications_gotify_priority_label()}</Label>
		<Select.Root
			type="single"
			value={priorityValue}
			{disabled}
			onValueChange={(value) => {
				values.priority = Number(value);
			}}
		>
			<Select.Trigger id="gotify-priority" class="h-10 w-full">
				<span>{selectedPriorityLabel}</span>
			</Select.Trigger>
			<Select.Content>
				{#each priorityOptions as option (option.value)}
					<Select.Item value={option.value}>{option.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<p class="text-muted-foreground text-sm">{m.notifications_gotify_priority_help()}</p>
	</div>

	<TextInputWithLabel
		bind:value={values.title}
		{disabled}
		label={m.notifications_gotify_title_label()}
		placeholder={m.notifications_gotify_title_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_gotify_title_help()}
	/>

	<SwitchWithLabel
		id="gotify-disable-tls"
		bind:checked={values.disableTls}
		{disabled}
		label={m.notifications_gotify_disable_tls_label()}
		description={m.notifications_gotify_disable_tls_help()}
	/>

	<EventSubscriptions
		providerId="gotify"
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
