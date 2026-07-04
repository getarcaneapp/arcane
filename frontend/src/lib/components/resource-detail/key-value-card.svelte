<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		children: Snippet;
		labelClass?: string;
		valueClass?: string;
		valueTitle?: string;
		cardClass?: string;
	}

	let {
		label,
		children,
		labelClass = 'text-muted-foreground text-[11px] font-semibold tracking-wide break-all uppercase',
		valueClass = 'text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all',
		valueTitle,
		cardClass
	}: Props = $props();
</script>

<!-- Borderless KV item: the surrounding panel supplies the glass surface. -->
<div class={cardClass}>
	<div class={labelClass}>{label}</div>
	<div class="mt-1 {valueClass}" title={valueTitle}>
		{@render children()}
	</div>
</div>
