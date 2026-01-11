<script lang="ts">
	import { Label } from '$lib/components/ui/label';
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
			<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
				<div>
					<Label class="text-base">{title}</Label>
					<p class="text-muted-foreground mt-1 text-sm">{description}</p>
				</div>
				<div class="space-y-4">
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
				</div>
			</div>
		</div>
	</div>
</div>
