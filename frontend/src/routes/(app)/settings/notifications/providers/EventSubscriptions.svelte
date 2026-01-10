<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		providerId: string;
		eventImageUpdate: boolean;
		eventContainerUpdate: boolean;
		disabled?: boolean;
	}

	let {
		providerId,
		eventImageUpdate = $bindable(),
		eventContainerUpdate = $bindable(),
		disabled = false
	}: Props = $props();
</script>

<div class="space-y-3 pt-2">
	<Label class="text-sm font-medium">{m.notifications_events_title()}</Label>
	<p class="text-muted-foreground text-xs">{m.notifications_events_description()}</p>
	<div class="space-y-2">
		<SwitchWithLabel
			id="{providerId}-event-image-update"
			bind:checked={eventImageUpdate}
			{disabled}
			label={m.notifications_event_image_update_label()}
			description={m.notifications_event_image_update_description()}
		/>
		<SwitchWithLabel
			id="{providerId}-event-container-update"
			bind:checked={eventContainerUpdate}
			{disabled}
			label={m.notifications_event_container_update_label()}
			description={m.notifications_event_container_update_description()}
		/>
	</div>
</div>
