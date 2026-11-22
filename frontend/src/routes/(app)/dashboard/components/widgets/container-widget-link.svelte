<script lang="ts">
	import { resolve } from '$app/paths';

	let {
		container,
		environmentId
	}: {
		container: { id: string; name: string } | undefined;
		environmentId: string;
	} = $props();
</script>

{#if container}
	<a
		href={resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(environmentId)}`, {
			containerId: container.id
		})}
		class="min-w-0 truncate text-xs text-muted-foreground hover:underline"
	>
		{container.name}
	</a>
{/if}
