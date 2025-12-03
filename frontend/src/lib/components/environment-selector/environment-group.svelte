<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import EnvironmentItem from './environment-item.svelte';
	import type { Environment, EnvironmentGroup } from './types';

	interface Props {
		group: EnvironmentGroup;
		selectedEnvId?: string;
		onSelect?: (env: Environment) => void;
	}

	let { group, selectedEnvId, onSelect }: Props = $props();
</script>

<Collapsible.Root class="w-full" open={false}>
	<Collapsible.Trigger
		class="hover:bg-muted/60 flex w-full items-center justify-between rounded-lg px-2.5 py-2 text-sm font-medium"
	>
		<span class="capitalize">{group.name}</span>
		<div class="flex items-center gap-2">
			<span class="text-muted-foreground text-xs">{group.items.length}</span>
			<ChevronDownIcon class="size-4 transition-transform [[data-state=open]>&]:rotate-180" />
		</div>
	</Collapsible.Trigger>
	<Collapsible.Content class="space-y-1 pt-1 pl-2">
		{#each group.items as env (env.id)}
			<EnvironmentItem {env} isSelected={selectedEnvId === env.id} {onSelect} />
		{/each}
	</Collapsible.Content>
</Collapsible.Root>
