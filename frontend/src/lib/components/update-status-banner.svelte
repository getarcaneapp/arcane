<script lang="ts">
	import type { Component } from 'svelte';

	interface Props {
		icon: Component;
		wrapperClass: string;
		gradientFrom: string;
		gradientTo: string;
		shadowColor: string;
		titleClass: string;
		descriptionClass: string;
		title: string;
		description: string;
	}

	let {
		icon: Icon,
		wrapperClass,
		gradientFrom,
		gradientTo,
		shadowColor,
		titleClass,
		descriptionClass,
		title,
		description
	}: Props = $props();
</script>

<div class={wrapperClass}>
	<div class="flex items-center gap-3">
		<div
			class="flex h-10 w-10 items-center justify-center rounded-full bg-linear-to-br {gradientFrom} {gradientTo} shadow-lg {shadowColor}"
		>
			<Icon class="size-5 text-white" />
		</div>
		<div>
			<div class="text-sm font-semibold {titleClass}">{title}</div>
			<div class="text-xs {descriptionClass}">{description}</div>
		</div>
	</div>
</div>
