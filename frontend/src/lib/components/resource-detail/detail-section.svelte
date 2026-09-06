<script lang="ts">
	import type { IconType } from '#lib/icons/index.js';
	import type { Snippet } from 'svelte';
	import { cn } from '#lib/utils.js';

	interface Props {
		title: string;
		icon?: IconType;
		class?: string;
		children: Snippet;
	}

	let { title, icon: Icon, class: className, children }: Props = $props();
</script>

<div class={cn('space-y-4 border-t pt-6', className)}>
	<h3 class="flex items-center gap-2 text-sm font-medium">
		{#if Icon}
			<Icon class="size-4" />
		{/if}
		{title}
	</h3>
	{@render children()}
</div>
