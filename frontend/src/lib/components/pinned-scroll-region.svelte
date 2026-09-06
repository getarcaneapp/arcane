<script lang="ts">
	import { cn } from '#lib/utils.js';
	import type { Snippet } from 'svelte';
	import type { Attachment } from 'svelte/attachments';

	interface Props {
		itemCount: number;
		threshold?: number;
		class?: string;
		children: Snippet;
	}

	let { itemCount, threshold = 24, class: className, children }: Props = $props();
	let pinnedToBottom = $state(true);

	const pinToBottom: Attachment<HTMLElement> = (node) => {
		function handleScroll() {
			pinnedToBottom = node.scrollHeight - node.scrollTop - node.clientHeight < threshold;
		}

		node.addEventListener('scroll', handleScroll, { passive: true });
		$effect(() => {
			void itemCount;
			if (!pinnedToBottom) return;
			queueMicrotask(() => {
				if (node.isConnected && pinnedToBottom) node.scrollTop = node.scrollHeight;
			});
		});

		return () => node.removeEventListener('scroll', handleScroll);
	};
</script>

<div class={cn(className)} {@attach pinToBottom}>
	{@render children()}
</div>
