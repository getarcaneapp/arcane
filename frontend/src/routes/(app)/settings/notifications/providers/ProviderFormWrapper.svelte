<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import { Switch } from '$lib/components/ui/switch/index.js';

	interface Props {
		id: string;
		title: string;
		description: string;
		enabledLabel: string;
		enabled: boolean;
		disabled?: boolean;
		children?: import('svelte').Snippet;
	}

	let { id, title, description, enabledLabel, enabled = $bindable(), disabled = false, children }: Props = $props();
</script>

<div class="space-y-4">
	<h3 class="text-lg font-medium">{title}</h3>
	<div class="bg-card rounded-lg border shadow-sm">
		<div class="space-y-6 p-6">
			<SettingsRow label={title} {description} contentClass="space-y-4">
				<div class="flex items-center gap-2">
					<Switch id="{id}-enabled" bind:checked={enabled} {disabled} />
					<Label for="{id}-enabled" class="font-normal">
						{enabledLabel}
					</Label>
				</div>

				{#if enabled && children}
					<div class="space-y-4 pt-2">
						{@render children()}
					</div>
				{/if}
			</SettingsRow>
		</div>
	</div>
</div>
