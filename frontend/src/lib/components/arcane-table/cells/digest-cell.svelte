<script lang="ts">
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';

	let { value }: { value: unknown } = $props();
	const text = $derived(typeof value === 'string' ? value : '');
	const separator = $derived(text.indexOf(':'));
	const hash = $derived(text.slice(separator + 1));
	const isHash = $derived(separator > 0 && hash.length > 16);
</script>

{#if text}
	<span class="inline-flex h-6 max-w-full items-center gap-1 rounded-xl bg-muted pr-0.5 pl-2 font-mono text-xs" title={text}>
		{#if isHash}
			<span class="whitespace-nowrap"
				><span class="text-muted-foreground">{text.slice(0, separator + 1)}</span>{hash.slice(0, 8)}…{hash.slice(-4)}</span
			>
		{:else}
			<span class="truncate">{text}</span>
		{/if}
		<CopyButton {text} size="icon" class="size-5 shrink-0" />
	</span>
{:else}
	<span class="text-muted-foreground">-</span>
{/if}
