<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	import CodeBlockLine from './code-block-line.svelte';

	let {
		ref = $bindable(null),
		class: className,
		code,
		wrap = false,
		lineNumbers = false,
		startLine = 1,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLPreElement>> & {
		/** Plain text to render. Split into lines when `lineNumbers` is set. */
		code?: string;
		wrap?: boolean;
		lineNumbers?: boolean;
		startLine?: number;
	} = $props();

	// A `code` string is split into lines so each one can be numbered.
	const lines = $derived(code !== undefined && lineNumbers ? code.replace(/\n$/, '').split('\n') : null);
</script>

{#snippet content()}
	<code
		data-slot="code-block-code"
		class=" grid min-h-lh min-w-max content-start in-data-numbered:grid-cols-[auto_1fr] in-data-wrap:min-w-0"
	>
		{#if lines}
			{#each lines as line, index (index)}
				<CodeBlockLine code={line} />
			{/each}
		{:else if code !== undefined}
			{code}
		{:else}
			{@render children?.()}
		{/if}
	</code>
{/snippet}

<!-- Whitespace inside a <pre> is rendered, so its content is a snippet defined outside it, where Svelte trims whitespace as usual. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<pre
	bind:this={ref}
	data-slot="code-block-content"
	data-wrap={wrap || undefined}
	data-numbered={lineNumbers || undefined}
	dir="ltr"
	tabindex={0}
	style:--code-block-start-line={startLine - 1}
	class={cn(
		'min-h-0 min-w-0 scroll-fade-x scrollbar-thin overflow-auto px-3 py-3 text-start font-code leading-relaxed outline-none [counter-reset:line_var(--code-block-start-line)] group-has-data-[slot=code-block-footer]/code-block:pb-2 group-has-data-[slot=code-block-header]/code-block:pt-2 group-data-[size=sm]/code-block:px-2.5 group-data-[size=sm]/code-block:py-2.5 group-data-[size=sm]/code-block:group-has-data-[slot=code-block-header]/code-block:pt-1.5 data-wrap:scroll-fade-y data-wrap:wrap-break-word data-wrap:whitespace-pre-wrap',
		className
	)}
	{...restProps}>{@render content()}</pre>
