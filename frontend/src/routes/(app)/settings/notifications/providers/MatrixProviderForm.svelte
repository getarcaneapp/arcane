<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Label } from '$lib/components/ui/label';
	import * as Select from '$lib/components/ui/select/index.js';
	import { m } from '$lib/paraglide/messages';
	import { ArrowDownIcon, SendEmailIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import type { MatrixFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';

	interface Props {
		values: MatrixFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: (testType?: string) => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const schema = z
		.object({
			enabled: z.boolean(),
			host: z.string(),
			port: z.coerce.number().int().min(0).max(65535),
			rooms: z.string(),
			username: z.string(),
			password: z.string(),
			disableTlsVerification: z.boolean(),
			eventImageUpdate: z.boolean(),
			eventContainerUpdate: z.boolean(),
			eventVulnerabilityFound: z.boolean(),
			eventPruneReport: z.boolean()
		})
		.superRefine((d, ctx) => {
			if (!d.enabled) return;
			if (!d.host.trim()) {
				ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['host'] });
			}
		});

	const validation = $derived.by(() => schema.safeParse(values));

	const fieldErrors = $derived.by(() => {
		const errs: Partial<Record<keyof MatrixFormValues, string>> = {};
		if (validation.success) return errs;
		for (const issue of validation.error.issues) {
			const key = issue.path?.[0] as keyof MatrixFormValues | undefined;
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
	id="matrix"
	title="Matrix"
	description={m.notifications_matrix_description()}
	enabledLabel={m.notifications_matrix_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<div class="grid grid-cols-1 gap-4 md:grid-cols-4">
		<div class="md:col-span-3">
			<TextInputWithLabel
				bind:value={values.host}
				{disabled}
				label={m.notifications_matrix_host_label()}
				placeholder={m.notifications_matrix_host_placeholder()}
				type="text"
				autocomplete="off"
				helpText={m.notifications_matrix_host_help()}
				error={fieldErrors.host}
			/>
		</div>
		<div class="md:col-span-1">
			<TextInputWithLabel
				bind:value={values.port}
				{disabled}
				label={m.notifications_matrix_port_label()}
				placeholder={m.notifications_matrix_port_placeholder()}
				type="number"
				autocomplete="off"
				helpText={m.notifications_matrix_port_help()}
				error={fieldErrors.port}
			/>
		</div>
	</div>

	<TextInputWithLabel
		bind:value={values.rooms}
		{disabled}
		label={m.notifications_matrix_rooms_label()}
		placeholder={m.notifications_matrix_rooms_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_matrix_rooms_help()}
		error={fieldErrors.rooms}
	/>

	<TextInputWithLabel
		bind:value={values.username}
		{disabled}
		label={m.notifications_matrix_username_label()}
		placeholder={m.notifications_matrix_username_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_matrix_username_help()}
		error={fieldErrors.username}
	/>

	<TextInputWithLabel
		bind:value={values.password}
		{disabled}
		label={m.notifications_matrix_password_label()}
		placeholder={m.notifications_matrix_password_placeholder()}
		type="password"
		autocomplete="off"
		helpText={m.notifications_matrix_password_help()}
		error={fieldErrors.password}
	/>

	<SwitchWithLabel
		id="matrix-disable-tls"
		bind:checked={values.disableTlsVerification}
		{disabled}
		label={m.notifications_matrix_disable_tls_label()}
		description={m.notifications_matrix_disable_tls_help()}
	/>

	<EventSubscriptions
		providerId="matrix"
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
