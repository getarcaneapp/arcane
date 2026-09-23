<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte.js';
	import { ansiToHtml } from '#lib/utils/formatting.js';
	import { m } from '#lib/paraglide/messages.js';
	import PinnedScrollRegion from '#lib/components/pinned-scroll-region.svelte';

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
</script>

<ResponsiveDialog
	bind:open={operationWatchStore.open}
	title={operationWatchStore.title}
	contentClass="sm:max-w-275"
	class="min-h-0"
>
	<div class="space-y-3 pb-4">
		<PinnedScrollRegion
			itemCount={operationWatchStore.lines.length}
			class="dark max-h-screen-70 min-h-70 overflow-auto rounded-lg border border-border/50 bg-background p-4 font-mono text-xs leading-relaxed text-foreground"
		>
			<!-- Lines only append within a session, so their positions are stable identities. -->
			{#each operationWatchStore.lines as line, idx (idx)}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
				<div class="break-words whitespace-pre-wrap">{@html ansiToHtml(line)}</div>
			{/each}
			{#if operationWatchStore.lines.length === 0}
				<div class="flex min-h-60 items-center justify-center text-muted-foreground">
					{m.activity_output_loading()}
				</div>
			{/if}
		</PinnedScrollRegion>

		{#if operationWatchStore.error}
			<div class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
				{operationWatchStore.error}
			</div>
		{/if}
	</div>
</ResponsiveDialog>
