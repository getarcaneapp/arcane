<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import { ArrowDownIcon } from '#lib/icons/index.js';

	let {
		title,
		description,
		open = $bindable(false),
		children
	}: {
		title: string;
		description: string;
		open?: boolean;
		children: Snippet;
	} = $props();
</script>

<div class="border-t border-border/50 pt-4">
	<Collapsible.Root bind:open>
		<Collapsible.Trigger>
			{#snippet child({ props })}
				<button
					{...props}
					type="button"
					class="flex w-full flex-wrap items-center gap-x-2 gap-y-1 rounded-xl text-left transition-colors hover:text-foreground"
				>
					<span class="text-sm font-medium">{title}</span>
					<span class="ml-auto text-xs text-muted-foreground">{description}</span>
					<ArrowDownIcon
						class={open
							? 'size-4 rotate-180 text-muted-foreground transition-transform'
							: 'size-4 text-muted-foreground transition-transform'}
					/>
				</button>
			{/snippet}
		</Collapsible.Trigger>
		<Collapsible.Content>
			<div class="mt-4 grid gap-4">
				{@render children()}
			</div>
		</Collapsible.Content>
	</Collapsible.Root>
</div>
