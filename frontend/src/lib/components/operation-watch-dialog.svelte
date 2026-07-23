<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte';
	import { ansiToHtml } from '#lib/utils/formatting';
	import { m } from '#lib/paraglide/messages';

	// Dismissing an attached session stops the project (the Ctrl-C of a
	// non-detached compose up), so every close attempt confirms first.
	operationWatchStore.setCloseRequestHandler(() => {
		openConfirmDialog({
			title: m.watch_close_confirm_title(),
			message: m.watch_close_confirm_message(),
			confirm: {
				label: m.common_stop(),
				destructive: true,
				button: 'stop',
				action: () => {
					operationWatchStore.forceClose();
				}
			}
		});
	});

	let container = $state<HTMLElement | null>(null);
	let pinnedToBottom = $state(true);

	function handleScroll() {
		if (!container) {
			return;
		}
		pinnedToBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 24;
	}

	$effect(() => {
		operationWatchStore.lines.length;
		if (!container || !pinnedToBottom) {
			return;
		}
		queueMicrotask(() => {
			if (container && pinnedToBottom) {
				container.scrollTop = container.scrollHeight;
			}
		});
	});
</script>

<ResponsiveDialog
	bind:open={operationWatchStore.open}
	title={operationWatchStore.title}
	contentClass="sm:max-w-[1100px]"
	class="min-h-0"
>
	<div class="space-y-3 pb-4">
		<div
			bind:this={container}
			onscroll={handleScroll}
			class="max-h-[70vh] min-h-[280px] overflow-auto rounded-lg border border-border/50 bg-zinc-950 p-4 font-mono text-[12px] leading-relaxed text-zinc-100"
		>
			{#each operationWatchStore.lines as line, idx (idx)}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
				<div class="break-words whitespace-pre-wrap">{@html ansiToHtml(line)}</div>
			{/each}
			{#if operationWatchStore.lines.length === 0}
				<div class="flex min-h-[240px] items-center justify-center text-zinc-500">
					{m.activity_output_loading()}
				</div>
			{/if}
		</div>

		{#if operationWatchStore.error}
			<div class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
				{operationWatchStore.error}
			</div>
		{/if}
	</div>
</ResponsiveDialog>
