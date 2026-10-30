<script lang="ts" module>
	import { tv, type VariantProps } from 'tailwind-variants';

	export const codeBlockLineVariants = tv({
		base: "-mx-3 px-3 in-data-numbered:gap-x-3 before:min-w-6 before:text-end before:text-muted-foreground after:start-1 after:text-muted-foreground group-data-[size=sm]/code-block:-mx-2.5 group-data-[size=sm]/code-block:px-2.5 relative block min-h-lh after:absolute after:top-0 after:select-none in-data-numbered:col-span-2 in-data-numbered:grid in-data-numbered:grid-cols-subgrid in-data-numbered:[counter-increment:line] in-data-numbered:before:content-[counter(line)] in-data-numbered:before:select-none in-data-numbered:supports-[content:counter(line)_/_'']:before:content-[counter(line)_/_'']",
		variants: {
			variant: {
				default: '',
				added: "bg-muted after:content-['+']",
				removed: "bg-destructive/10 after:text-destructive after:content-['−']",
				highlighted: 'bg-muted'
			}
		},
		defaultVariants: {
			variant: 'default'
		}
	});

	export type CodeBlockLineVariant = VariantProps<typeof codeBlockLineVariants>['variant'];

	// One highlighted token, such as one returned by `useHighlightedCode`.
	export type CodeBlockLineToken = {
		content: string;
		class?: string;
	};
</script>

<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		code,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLSpanElement>> & {
		variant?: CodeBlockLineVariant;
		/** The line as plain text, or as highlighted tokens. When it is set, `children` is ignored. */
		code?: string | CodeBlockLineToken[];
	} = $props();

	const newline = '\n';
</script>

<!-- An empty line holds a newline so copying a selection keeps blank lines. -->
<span
	bind:this={ref}
	data-slot="code-block-line"
	data-variant={variant}
	class={cn(codeBlockLineVariants({ variant }), className)}
	{...restProps}
>
	<span data-slot="code-block-line-content" class="min-w-0">
		{#if typeof code === 'string'}
			{code || newline}
		{:else if code?.length}
			{#each code as token, index (index)}
				<span class={token.class}>{token.content}</span>
			{/each}
		{:else if code}
			{newline}
		{:else if children}
			{@render children()}
		{:else}
			{newline}
		{/if}
	</span>
</span>
