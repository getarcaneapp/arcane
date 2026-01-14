<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SendEmailIcon } from '$lib/icons';
	import type { AppriseFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';

	interface Props {
		values: AppriseFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: () => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	export function isValid(): boolean {
		if (!values.enabled) return true;
		return values.apiUrl.trim().length > 0;
	}
</script>

<ProviderFormWrapper
	id="apprise"
	title={m.notifications_apprise_title()}
	description={m.notifications_apprise_description()}
	enabledLabel={m.notifications_apprise_enabled_label()}
	bind:enabled={values.enabled}
	{disabled}
>
	<TextInputWithLabel
		bind:value={values.apiUrl}
		{disabled}
		label={m.notifications_apprise_api_url_label()}
		placeholder={m.notifications_apprise_api_url_placeholder()}
		type="url"
		autocomplete="off"
		helpText={m.notifications_apprise_api_url_help()}
	/>
	<TextInputWithLabel
		bind:value={values.imageUpdateTag}
		{disabled}
		label={m.notifications_apprise_image_tag_label()}
		placeholder={m.notifications_apprise_image_tag_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_apprise_image_tag_help()}
	/>
	<TextInputWithLabel
		bind:value={values.containerUpdateTag}
		{disabled}
		label={m.notifications_apprise_container_tag_label()}
		placeholder={m.notifications_apprise_container_tag_placeholder()}
		type="text"
		autocomplete="off"
		helpText={m.notifications_apprise_container_tag_help()}
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
