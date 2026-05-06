<script lang="ts">
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import { Switch } from '$lib/components/ui/switch/index.js';

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

<div class="space-y-4">
	<h3 class="text-base font-semibold">{title}</h3>
	<SettingsRow label={title} {description} layout="inline">
		<Switch id="{id}-enabled" bind:checked={enabled} {disabled} />
	</SettingsRow>

	{#if enabled && children}
		<div class="border-border/60 space-y-4 border-l-2 pl-5">
			{@render children()}
		</div>
	{/if}
</div>
