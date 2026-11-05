<script lang="ts">
	import { LayoutGridIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	let {
		showLabels = true,
		onclick
	}: {
		showLabels?: boolean;
		onclick: () => void;
	} = $props();
</script>

<button
	type="button"
	class={cn(
		'bg-primary text-primary-foreground shadow-md hover:bg-primary/90 focus-visible:ring-ring',
		'flex-shrink-0 rounded-full transition-all duration-200 ease-out active:scale-95',
		'flex items-center justify-center',
		showLabels ? 'size-13 sm:size-14' : 'size-12 sm:size-13'
	)}
	aria-label={m.mobile_navigation()}
	{onclick}
	data-testid="mobile-nav-open"
>
	<LayoutGridIcon class={showLabels ? 'size-8 sm:size-8.5' : 'size-7.5 sm:size-8'} aria-hidden="true" />
	<span class="sr-only">{m.mobile_navigation()}</span>
</button>
