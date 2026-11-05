<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';

	interface Props {
		id: string;
		title: string;
		description: string;
		enabled: boolean;
		disabled?: boolean;
		children?: import('svelte').Snippet;
	}

	let { id, title, description, enabled = $bindable(), disabled = false, children }: Props = $props();
</script>

<SettingsSection>
	<SettingsRow for="{id}-enabled" label={title} {description} layout="switch">
		{#snippet children({ describedBy })}
			<Switch id="{id}-enabled" bind:checked={enabled} {disabled} aria-describedby={describedBy} />
		{/snippet}
	</SettingsRow>
	{#if enabled && children}
		{@render children()}
	{/if}
</SettingsSection>
