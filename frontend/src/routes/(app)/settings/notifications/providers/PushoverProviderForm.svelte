<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { Label } from '$lib/components/ui/label';
	import Textarea from '$lib/components/ui/textarea/textarea.svelte';
	import * as Select from '$lib/components/ui/select/index.js';
	import { m } from '$lib/paraglide/messages';
	import { ArrowDownIcon, SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { PushoverFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: PushoverFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: (testType?: string) => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const priorityOptions = [
		{ value: '-2', label: '-2' },
		{ value: '-1', label: '-1' },
		{ value: '0', label: '0' },
		{ value: '1', label: '1' },
		{ value: '2', label: '2' }
	];

	const priorityValue = $derived(String(values.priority ?? 0));

	const schema = z
		.object({
			enabled: z.boolean(),
			token: z.string(),
			user: z.string(),
			devices: z.string(),
			priority: z.coerce.number().int().min(-2).max(2),
			title: z.string(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean(),
			eventVulnerabilityFound: z.boolean(),
			eventPruneReport: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.token.trim()) {
				ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['token'] });
			}
			if (!d.user.trim()) {
				ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['user'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const selectedPriorityLabel = $derived(
		priorityOptions.find((option) => option.value === priorityValue)?.label ?? priorityValue
	);

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof PushoverFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof PushoverFormValues | undefined;
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
	id="pushover"
	title="Pushover"
	description={m.notifications_pushover_description()}
	enabledLabel={m.notifications_pushover_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.token}
		{disabled}
		label={m.notifications_pushover_token_label()}
		placeholder={m.notifications_pushover_token_placeholder()}
		type="password"
		autocomplete="off"
		helpText={m.notifications_pushover_token_help()}
		error={fieldErrors.token}
	/>

	<TextInputWithLabel
		bind:value={values.user}
		{disabled}
		label={m.notifications_pushover_user_label()}
		placeholder={m.notifications_pushover_user_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_pushover_user_help()}
		error={fieldErrors.user}
	/>

	<div class="space-y-2">
		<Label for="pushover-devices">{m.notifications_pushover_devices_label()}</Label>
		<Textarea
			id="pushover-devices"
			bind:value={values.devices}
			{disabled}
			autocomplete="off"
			placeholder={m.notifications_pushover_devices_placeholder()}
			rows={2}
		/>
		<p class="text-muted-foreground text-sm">{m.notifications_pushover_devices_help()}</p>
	</div>

	<div class="space-y-2">
		<Label for="pushover-priority">{m.notifications_pushover_priority_label()}</Label>
		<Select.Root
			type="single"
			value={priorityValue}
			{disabled}
			onValueChange={(value) => {
				values.priority = Number(value);
			}}
		>
			<Select.Trigger id="pushover-priority" class="h-10 w-full">
				<span>{selectedPriorityLabel}</span>
			</Select.Trigger>
			<Select.Content>
				{#each priorityOptions as option (option.value)}
					<Select.Item value={option.value}>{option.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<p class="text-muted-foreground text-sm">{m.notifications_pushover_priority_help()}</p>
	</div>

	<TextInputWithLabel
		bind:value={values.title}
		{disabled}
		label={m.notifications_pushover_title_label()}
		placeholder={m.notifications_pushover_title_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_pushover_title_help()}
	/>

	<EventSubscriptions
		providerId="pushover"
		bind:eventImageUpdate={values.eventImageUpdate}
		bind:eventContainerUpdate={values.eventContainerUpdate}
		bind:eventVulnerabilityFound={values.eventVulnerabilityFound}
		bind:eventPruneReport={values.eventPruneReport}
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
					<DropdownMenu.Item onclick={() => onTest()}>
						<SendEmailIcon class="size-4" />
						{m.notifications_test_notification()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => onTest('vulnerability-found')}>
						<SendEmailIcon class="size-4" />
						{m.notifications_test_vulnerability_notification()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => onTest('prune-report')}>
						<SendEmailIcon class="size-4" />
						{m.notifications_test_prune_report_notification()}
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	{/if}
</ProviderFormWrapper>
