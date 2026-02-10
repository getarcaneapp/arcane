<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import type { AppriseFormValues } from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import DynamicProviderFormBuilder from './DynamicProviderFormBuilder.svelte';
	import NotificationProviderTestMenu from './NotificationProviderTestMenu.svelte';
	import type { ProviderFormSchema } from './provider-form-schema';

	interface Props {
		values: AppriseFormValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: (testType?: string) => void;
	}

	let { values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const formSchema: ProviderFormSchema<AppriseFormValues> = [
		{
			kind: 'input',
			key: 'apiUrl',
			id: 'apprise-api-url',
			label: m.notifications_apprise_api_url_label(),
			placeholder: m.notifications_apprise_api_url_placeholder(),
			helpText: m.notifications_apprise_api_url_help(),
			inputType: 'url'
		},
		{
			kind: 'input',
			key: 'imageUpdateTag',
			id: 'apprise-image-update-tag',
			label: m.notifications_apprise_image_tag_label(),
			placeholder: m.notifications_apprise_image_tag_placeholder(),
			helpText: m.notifications_apprise_image_tag_help()
		},
		{
			kind: 'input',
			key: 'containerUpdateTag',
			id: 'apprise-container-update-tag',
			label: m.notifications_apprise_container_tag_label(),
			placeholder: m.notifications_apprise_container_tag_placeholder(),
			helpText: m.notifications_apprise_container_tag_help()
		}
	];

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
	<DynamicProviderFormBuilder bind:values {disabled} schema={formSchema} />

	<NotificationProviderTestMenu {disabled} {isTesting} {onTest} />
</ProviderFormWrapper>
