<script lang="ts">
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import { MobileNavDrawerIcon } from '$lib/icons';

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
		'bg-primary text-primary-foreground hover:bg-primary/90 focus-visible:ring-ring/50 shadow-md',
		'flex-shrink-0 rounded-full transition-all duration-200 ease-out active:scale-95',
		'flex items-center justify-center',
		// Keep a large tap target but allow a slightly smaller footprint on the smallest screens.
		'size-[52px] sm:size-14'
	)}
	aria-label={m.mobile_navigation()}
	{onclick}
	data-testid="mobile-nav-open"
>
	<MobileNavDrawerIcon class="size-8 sm:size-8.5" aria-hidden="true" />
	<span class="sr-only">{m.mobile_navigation()}</span>
</button>
