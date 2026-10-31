<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as Popover from '#lib/components/ui/popover/index.js';
	import { ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { MixedSettingValue } from '#lib/types/settings.js';

	let { values, class: className, children }: { values: MixedSettingValue[]; class?: string; children: Snippet } = $props();
</script>

<Popover.Root>
	<Popover.Trigger>
		{#snippet child({ props })}
			<button {...props} type="button" title={m.mixed_across_selected_environments()} class={className}>
				{@render children()}
			</button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content align="start" variant="panel" class="w-80 overflow-hidden" onOpenAutoFocus={(event) => event.preventDefault()}>
		<p class="px-4 pt-3 pb-2 text-xs text-muted-foreground">{m.mixed_across_selected_environments()}</p>
		<div class="divide-y divide-border/50 border-t border-border/50">
			{#each values as value, index (index)}
				<Popover.Close onclick={value.apply}>
					{#snippet child({ props })}
						<button
							{...props}
							type="button"
							class="flex w-full items-center justify-between gap-4 px-4 py-3 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
						>
							<span class="min-w-0">
								<span class="block truncate text-sm font-medium">{value.label}</span>
								<span class="block truncate text-xs text-muted-foreground">{value.environments.join(', ')}</span>
							</span>
							<span class="flex shrink-0 items-center gap-0.5 text-xs text-muted-foreground">
								{m.use_for_all()}
								<ArrowRightIcon class="size-3.5" />
							</span>
						</button>
					{/snippet}
				</Popover.Close>
			{/each}
		</div>
	</Popover.Content>
</Popover.Root>
