<!--
	Installed from @ieedan/shadcn-svelte-extras
-->

<script lang="ts" module>
	import { cn } from '#lib/utils';
	import { tv, type VariantProps } from 'tailwind-variants';
	import { CopyButton } from '#lib/components/ui/copy-button';
	import type { UseClipboard } from '#lib/hooks/use-clipboard.svelte';

	const style = tv({
		base: 'bg-background relative w-full max-w-full rounded-md border py-2.5 pr-12 pl-3',
		variants: {
			variant: {
				default: 'backdrop-blur-sm bg-card/90',
				secondary: 'border-border bg-accent',
				destructive: 'border-destructive bg-destructive',
				primary: 'border-primary bg-primary text-primary-foreground'
			}
		}
	});

	type Variant = VariantProps<typeof style>['variant'];

	export type SnippetProps = {
		variant?: Variant;
		text: string | string[];
		class?: string;
		onCopy?: (status: UseClipboard['status']) => void;
	};
</script>

<script lang="ts">
	let { text, variant = 'default', onCopy, class: className }: SnippetProps = $props();
</script>

<div class={cn(style({ variant, className: className }))}>
	{#if typeof text == 'string'}
		<pre class={cn('overflow-y-auto text-left font-mono text-sm font-light whitespace-nowrap')}>
			{text}
		</pre>
	{:else}
		{#each text as line, i (i)}
			<pre class={cn('overflow-y-auto text-left font-mono text-sm font-light whitespace-nowrap')}>
			{line}
		</pre>
		{/each}
	{/if}

	<CopyButton
		size="icon"
		variant="ghost"
		class="hover:text-opacity-80 absolute top-1/2 right-2 size-7 -translate-y-1/2 transition-opacity ease-in-out hover:bg-transparent dark:hover:bg-transparent"
		text={typeof text === 'string' ? text : text.join('\n')}
		{onCopy}
	/>
</div>
