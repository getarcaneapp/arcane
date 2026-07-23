<script lang="ts">
	import type { ActivityMessage } from '#lib/types/activity.type';
	import { ansiToHtml } from '#lib/utils/formatting';
	import { cn } from '#lib/utils';

	let { messages }: { messages: ActivityMessage[] } = $props();

	let container = $state<HTMLElement | null>(null);
	let pinnedToBottom = $state(true);

	function handleScroll() {
		if (!container) {
			return;
		}
		pinnedToBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 24;
	}

	$effect(() => {
		messages.length;
		if (!container || !pinnedToBottom) {
			return;
		}
		queueMicrotask(() => {
			if (container && pinnedToBottom) {
				container.scrollTop = container.scrollHeight;
			}
		});
	});

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

<div bind:this={container} onscroll={handleScroll} class="max-h-80 min-h-40 overflow-auto px-5 py-4">
	{#each messages as message (message.id)}
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
		<div class={cn('wrap-break-word whitespace-pre-wrap', lineClassInternal(message.level))}>
			{@html ansiToHtml(message.message)}
		</div>
	{/each}
</div>
