<script lang="ts">
	import type { ComponentProps } from 'svelte';

	import ResourceName from '#lib/components/resource-name.svelte';

	let {
		dockerName,
		watchHostname,
		...nameProps
	}: Omit<ComponentProps<typeof ResourceName>, 'tooltip'> & {
		dockerName: string;
		watchHostname: () => (() => void) | undefined;
	} = $props();

	$effect(() => watchHostname());
</script>

<ResourceName {...nameProps}>
	{#snippet tooltip()}
		{#if nameProps.name !== dockerName}
			<span class="block max-w-tooltip break-all">{nameProps.name}</span>
		{/if}
		<span class="block max-w-tooltip break-all">{dockerName}</span>
	{/snippet}
</ResourceName>
