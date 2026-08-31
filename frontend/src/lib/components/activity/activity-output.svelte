<script lang="ts">
	import type { ActivityMessage } from '#lib/types/activity.type';
	import { ansiToHtml } from '#lib/utils/formatting';
	import { cn } from '#lib/utils';
	import PinnedScrollRegion from '#lib/components/pinned-scroll-region.svelte';

	let { messages }: { messages: ActivityMessage[] } = $props();

	function lineClassInternal(level: ActivityMessage['level']): string {
		switch (level) {
			case 'error':
				return 'text-red-300';
			case 'warning':
				return 'text-amber-300';
			default:
				return 'text-zinc-100';
		}
	}
</script>

<PinnedScrollRegion itemCount={messages.length} class="max-h-80 min-h-40 overflow-auto px-5 py-4">
	{#each messages as message (message.id)}
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
		<div class={cn('wrap-break-word whitespace-pre-wrap', lineClassInternal(message.level))}>
			{@html ansiToHtml(message.message)}
		</div>
	{/each}
</PinnedScrollRegion>
