<script lang="ts" module>
	import { tv, type VariantProps } from 'tailwind-variants';

	export const emptyMediaVariants = tv({
		base: 'mb-2 flex shrink-0 items-center justify-center [&_svg]:pointer-events-none [&_svg]:shrink-0',
		variants: {
			variant: {
				default: 'bg-transparent',
				icon: "flex size-13 shrink-0 items-center justify-center rounded-xl border border-primary/25 bg-primary/5 text-primary ring-6 ring-primary/5 [&_svg:not([class*='size-'])]:size-6"
			}
		},
		defaultVariants: {
			variant: 'default'
		}
	});

	export type EmptyMediaVariant = VariantProps<typeof emptyMediaVariants>['variant'];
</script>

<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn } from '#lib/utils.js';

	let {
		class: className,
		children,
		variant = 'default',
		...restProps
	}: HTMLAttributes<HTMLDivElement> & { variant?: EmptyMediaVariant } = $props();
</script>

<div data-slot="empty-icon" data-variant={variant} class={cn(emptyMediaVariants({ variant }), className)} {...restProps}>
	{@render children?.()}
</div>
