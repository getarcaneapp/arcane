<script lang="ts">
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { cn } from '$lib/utils';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';

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
</script>

<ArcaneButton
	action="base"
	tone="ghost"
	size={showLabels ? 'sm' : 'icon'}
	href={item.url}
	aria-label={`${item.title}${active ? ' (current page)' : ''}`}
	aria-current={active ? 'page' : undefined}
	class={cn(
		'min-w-0 flex-1 transition-all duration-200 ease-out',
		'hover:bg-muted/50 hover:text-foreground hover:scale-[1.02]',
		'focus-visible:ring-muted-foreground/50 focus-visible:ring-1 focus-visible:ring-offset-1',
		'focus-visible:scale-[1.02] focus-visible:ring-offset-transparent',
		'active:scale-[0.98]',
		showLabels
			? 'h-11 flex-col gap-0.5 overflow-hidden rounded-2xl px-2 py-1 sm:h-12 sm:min-w-[60px] sm:px-3 sm:py-1.5'
			: 'h-11 w-11 rounded-2xl',
		active && 'bg-muted text-foreground hover:bg-muted/70 shadow-sm',
		className
	)}
	data-testid="mobile-nav-item"
>
	{@const IconComponent = item.icon}
	<IconComponent size={showLabels ? 20 : 24} aria-hidden="true" />
	{#if showLabels}
		<span class="text-muted-foreground w-full truncate text-center text-[10px] leading-none font-normal">{item.title}</span>
	{:else}
		<span class="sr-only">{item.title}</span>
	{/if}
</ArcaneButton>
