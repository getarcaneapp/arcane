<script lang="ts">
	import ServerIcon from '@lucide/svelte/icons/server';
	import RouterIcon from '@lucide/svelte/icons/router';
	import CheckIcon from '@lucide/svelte/icons/check';
	import { cn } from '$lib/utils';
	import type { Environment } from './types';
	import { getStatusColor, getConnectionString } from './utils';

	interface Props {
		env: Environment;
		isSelected?: boolean;
		onSelect?: (env: Environment) => void;
	}

	let { env, isSelected = false, onSelect }: Props = $props();

	const isDisabled = $derived(!env.enabled);
</script>

<button
	class={cn(
		'group relative flex w-full items-center gap-3 rounded-lg p-2.5 text-left transition-all',
		isSelected ? 'bg-primary/10 ring-primary/40 ring-1' : isDisabled ? 'cursor-not-allowed opacity-50' : 'hover:bg-muted/60'
	)}
	onclick={() => onSelect?.(env)}
	disabled={isDisabled}
>
	<div class="relative">
		<div
			class={cn(
				'flex size-9 items-center justify-center rounded-md',
				isSelected ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
			)}
		>
			{#if env.id === '0'}<ServerIcon class="size-4" />{:else}<RouterIcon class="size-4" />{/if}
		</div>
		<span class={cn('ring-background absolute -top-0.5 -right-0.5 size-2.5 rounded-full ring-2', getStatusColor(env.status))}
		></span>
	</div>

	<div class="min-w-0 flex-1">
		<div class="flex items-center gap-2">
			<span class={cn('truncate text-sm font-medium', isSelected && 'text-primary-foreground')}>{env.name}</span>
			{#if isSelected}<CheckIcon class="text-primary size-4 shrink-0" />{/if}
		</div>
		<div class="text-muted-foreground truncate text-xs">{getConnectionString(env)}</div>
	</div>

	{#if env.tags?.length}
		<div class="hidden shrink-0 sm:flex sm:gap-1">
			{#each env.tags.slice(0, 2) as tag}
				<span class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px]">{tag}</span>
			{/each}
			{#if env.tags.length > 2}
				<span class="text-muted-foreground text-[10px]">+{env.tags.length - 2}</span>
			{/if}
		</div>
	{/if}
</button>
