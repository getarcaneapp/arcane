<script lang="ts">
	import PinnedScrollRegion from '#lib/components/pinned-scroll-region.svelte';
	import type { ActivityMessage } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';
	import { ansiToHtml } from '#lib/utils/formatting.js';

	let {
		messages,
		class: className
	}: {
		messages: ActivityMessage[];
		/** Height constraints; defaults suit an inline log. */
		class?: string;
	} = $props();

	function lineClassInternal(level: ActivityMessage['level']): string {
		switch (level) {
			case 'error':
				return 'text-destructive';
			case 'warning':
				return 'text-warning';
			default:
				return 'text-foreground';
		}
	}
</script>

<PinnedScrollRegion itemCount={messages.length} class={cn('max-h-80 min-h-40 overflow-auto px-4 py-3', className)}>
	{#each messages as message (message.id)}
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
		<div class={cn('wrap-break-word whitespace-pre-wrap', lineClassInternal(message.level))}>
			{@html ansiToHtml(message.message)}
		</div>
	{/each}
</PinnedScrollRegion>
