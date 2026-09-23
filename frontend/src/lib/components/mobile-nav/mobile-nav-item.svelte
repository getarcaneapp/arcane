<script lang="ts">
	import type { NavigationItem } from '#lib/config/navigation-config.js';
	import { cn } from '#lib/utils.js';

	let {
		item,
		active = false,
		showLabels = false,
		class: className = ''
	}: {
		item: NavigationItem;
		active?: boolean;
		showLabels?: boolean;
		class?: string;
	} = $props();

	const IconComponent = $derived(item.icon);
</script>

<a
	href={item.url}
	aria-label={`${item.title}${active ? ' (current page)' : ''}`}
	aria-current={active ? 'page' : undefined}
	class={cn(
		"inline-flex min-w-0 flex-1 shrink-0 items-center justify-center border border-transparent bg-transparent text-sm font-medium whitespace-nowrap text-foreground outline-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
		'transition-all duration-200 ease-out',
		'hover:scale-102 hover:bg-muted/50 hover:text-foreground',
		'focus-visible:scale-102 focus-visible:ring-1 focus-visible:ring-muted-foreground/50 focus-visible:ring-offset-1 focus-visible:ring-offset-transparent',
		'active:scale-98',
		showLabels
			? 'h-11 flex-col gap-0.5 overflow-hidden rounded-2xl px-2.5 py-1 sm:h-12 sm:min-w-15 sm:px-3 sm:py-1.5'
			: 'size-11 gap-2 rounded-2xl',
		active && 'bg-muted shadow-sm hover:bg-muted/70',
		className
	)}
	data-testid="mobile-nav-item"
>
	<IconComponent size={showLabels ? 20 : 24} aria-hidden="true" />
	{#if showLabels}
		<span class="w-full truncate text-center text-3xs leading-none font-normal text-muted-foreground">{item.title}</span>
	{:else}
		<span class="sr-only">{item.title}</span>
	{/if}
</a>
