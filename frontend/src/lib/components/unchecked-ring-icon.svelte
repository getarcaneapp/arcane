<script lang="ts">
	import type { SVGAttributes } from 'svelte/elements';

	import { cn } from '#lib/utils.js';

	let { class: className, ...restProps }: SVGAttributes<SVGSVGElement> = $props();
</script>

<svg viewBox="0 0 16 16" class={cn('size-4', className)} fill="none" aria-hidden="true" {...restProps}>
	<circle cx="8" cy="8" r="6.25" stroke="currentColor" stroke-width="1.5" pathLength="32" stroke-dasharray="2 2" />
	<circle cx="8" cy="8" r="2.25" fill="currentColor" />
</svg>
