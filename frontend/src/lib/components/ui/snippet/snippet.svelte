<!--
	Installed from @ieedan/shadcn-svelte-extras
-->

<script lang="ts" module>
	import { tv, type VariantProps } from 'tailwind-variants';

	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import type { UseClipboard } from '#lib/hooks/use-clipboard.svelte.js';
	import { cn } from '#lib/utils.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';

	const style = tv({
		base: 'bg-background flex w-full max-w-full items-center gap-2 rounded-md border py-1.5 pr-1.5 pl-3',
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
	<div class="min-w-0 flex-1">
		{#if typeof text == 'string'}
			<pre class="scrollbar-hide overflow-x-auto text-left font-mono text-sm font-light whitespace-nowrap">{text}</pre>
		{:else}
			{#each withOccurrenceKeys(text) as line (line.key)}
				<pre class="scrollbar-hide overflow-x-auto text-left font-mono text-sm font-light whitespace-nowrap">{line.value}</pre>
			{/each}
		{/if}
	</div>

	<CopyButton
		size="icon"
		variant="ghost"
		class="size-7 shrink-0 transition-opacity ease-in-out hover:bg-transparent hover:opacity-80 dark:hover:bg-transparent"
		text={typeof text === 'string' ? text : text.join('\n')}
		{onCopy}
	/>
</div>
