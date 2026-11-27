<script lang="ts">
	import type { ClassValue } from 'svelte/elements';

	import * as Empty from '#lib/components/ui/empty/index.js';
	import type { IconType } from '#lib/icons/index.js';

	interface Props {
		icon: IconType;
		title: string;
		description?: string;
		class?: ClassValue;
	}

	let { icon: Icon, title, description, class: className }: Props = $props();
</script>

<Empty.Root class={className} role="status" aria-live="polite" aria-busy="true">
	<Empty.Header>
		<Empty.Media class="relative size-16">
			<span class="absolute inset-0 rounded-full border-2 border-primary-tint/15"></span>
			<span
				class="absolute inset-0 animate-spin rounded-full border-2 border-transparent border-t-primary border-r-primary/40 motion-reduce:animate-none"
			></span>
			<span
				class="flex size-11 items-center justify-center rounded-full bg-primary-tint/10 text-primary-text motion-safe:animate-pulse"
			>
				<Icon class="size-5" aria-hidden="true" />
			</span>
		</Empty.Media>
		<Empty.Title>{title}</Empty.Title>
		{#if description}
			<Empty.Description>{description}</Empty.Description>
		{/if}
	</Empty.Header>
</Empty.Root>
