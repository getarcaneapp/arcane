<script lang="ts" module>
	import { tv, type VariantProps } from 'tailwind-variants';

	const inputGroupButtonVariants = tv({
		base: 'flex items-center gap-2 text-sm shadow-none',
		variants: {
			size: {
				xs: "h-6 gap-1 rounded-[calc(var(--radius)-5px)] px-2 has-[>svg]:px-2 [&>svg:not([class*='size-'])]:size-3.5",
				sm: 'h-8 gap-1.5 rounded-md px-2.5 has-[>svg]:px-2.5',
				'icon-xs': 'size-6 rounded-[calc(var(--radius)-5px)] p-0 has-[>svg]:p-0',
				'icon-sm': 'size-8 p-0 has-[>svg]:p-0'
			}
		},
		defaultVariants: {
			size: 'xs'
		}
	});

	export type InputGroupButtonSize = VariantProps<typeof inputGroupButtonVariants>['size'];
</script>

<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';

	interface Props {
		class?: string;
		children?: import('svelte').Snippet;
		type?: 'button' | 'submit' | 'reset';
		variant?: 'ghost' | 'outline';
		size?: InputGroupButtonSize;
		[key: string]: any;
	}

	let { class: className, children, type = 'button', variant = 'ghost', size = 'xs', ...restProps }: Props = $props();
</script>

<ArcaneButton
	action="base"
	{type}
	data-size={size}
	tone={variant === 'ghost' ? 'ghost' : 'outline'}
	class={cn(inputGroupButtonVariants({ size }), className)}
	{...restProps}
>
	{@render children?.()}
</ArcaneButton>
