<script lang="ts">
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { cn } from '#lib/utils.js';

	let { value, wrap = false }: { value: unknown; wrap?: boolean } = $props();
	const text = $derived(typeof value === 'string' ? value : '');
	const separator = $derived(text.indexOf(':'));
</script>

{#if text}
	<span
		class={cn(
			'inline-flex min-h-7 items-center gap-1 rounded-xl bg-muted pr-0.5 pl-2 font-mono text-xs-plus leading-5',
			wrap ? 'max-w-full py-1 break-all' : 'whitespace-nowrap'
		)}
	>
		<span class={wrap ? 'min-w-0' : undefined}
			><span class="text-muted-foreground">{text.slice(0, separator + 1)}</span>{text.slice(separator + 1)}</span
		>
		<CopyButton {text} size="icon" class="size-5 shrink-0" />
	</span>
{:else}
	<span class="text-muted-foreground">-</span>
{/if}
