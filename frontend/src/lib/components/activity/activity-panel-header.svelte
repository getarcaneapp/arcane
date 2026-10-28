<script lang="ts">
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import type { Snippet } from 'svelte';

	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';

	let {
		titleId,
		dialogTitle = false,
		leading,
		children
	}: {
		titleId?: string;
		/** Register the heading as the enclosing dialog's (or drawer's) accessible title. */
		dialogTitle?: boolean;
		leading?: Snippet;
		children?: Snippet;
	} = $props();

	const titleClass = 'min-w-0 truncate text-base font-semibold text-foreground';
</script>

<div class="flex h-12 shrink-0 items-center gap-1 pr-2 pl-4">
	{@render leading?.()}
	{#if dialogTitle}
		<DialogPrimitive.Title>
			{#snippet child({ props })}
				<h2 {...props} class={titleClass}>{m.activity_center_title()}</h2>
			{/snippet}
		</DialogPrimitive.Title>
	{:else}
		<h2 id={titleId} class={titleClass}>{m.activity_center_title()}</h2>
	{/if}
	{#if activityStore.activeCount > 0}
		<span class="ml-1 rounded-full bg-info/15 px-2 py-0.5 text-2xs font-semibold text-info tabular-nums">
			{m.activity_active_count({ count: activityStore.activeCount })}
		</span>
	{/if}
	<div class="ml-auto flex items-center">
		{@render children?.()}
	</div>
</div>
