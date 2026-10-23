<script lang="ts">
	import type { Component } from 'svelte';

	interface Props {
		icon: Component;
		tileClass: string;
		titleClass: string;
		title: string;
		description: string;
	}

	let { icon: Icon, tileClass, titleClass, title, description }: Props = $props();
</script>

<div class="mx-1.5 flex items-center gap-3 border-sidebar-border px-1.5 pt-3 pb-2.5 not-last:border-b last:pb-3">
	<div class="grid size-8 shrink-0 place-items-center rounded-full bg-sidebar-accent {tileClass}">
		<Icon class="size-4" />
	</div>
	<div class="min-w-0 flex-1">
		<div class="text-sm font-semibold {titleClass}">{title}</div>
		<div class="text-xs wrap-break-word text-muted-foreground">{description}</div>
	</div>
</div>
