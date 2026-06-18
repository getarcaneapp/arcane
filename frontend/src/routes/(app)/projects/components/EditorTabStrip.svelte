<script lang="ts">
	import type { Snippet } from 'svelte';
	import { CloseIcon, FileTextIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';

	interface EditorTab {
		key: string;
		label: string;
		title: string;
		iconClass: string;
		pending: boolean;
	}

	interface Props {
		tabs: EditorTab[];
		activeKey: string;
		onSelect: (key: string) => void;
		onClose: (key: string) => void;
		actions?: Snippet;
	}

	let { tabs, activeKey, onSelect, onClose, actions }: Props = $props();
</script>

<div class="border-border bg-muted/30 flex h-9 shrink-0 items-center border-b">
	<div class="scrollbar-hide flex h-full min-w-0 flex-1 items-center overflow-x-auto">
		{#each tabs as tab (tab.key)}
			{@const isActive = activeKey === tab.key}
			<div
				class={cn(
					'group border-border relative flex h-full shrink-0 items-center border-r',
					isActive ? 'bg-card' : 'hover:bg-accent/50'
				)}
				data-tab-key={tab.key}
				data-active={isActive}
			>
				{#if isActive}
					<span class="bg-primary absolute inset-x-0 top-0 h-0.5"></span>
				{/if}
				<button
					type="button"
					class={cn(
						'flex h-full items-center gap-1.5 pr-1 pl-3 text-[13px]',
						isActive ? 'text-foreground' : 'text-muted-foreground'
					)}
					title={tab.title}
					onclick={() => onSelect(tab.key)}
				>
					<FileTextIcon class={cn('size-3.5 shrink-0', tab.iconClass)} />
					<span class="max-w-40 truncate">{tab.label}</span>
					{#if tab.pending}
						<span
							class="bg-primary size-1.5 shrink-0 rounded-full"
							role="img"
							aria-label={m.common_unsaved_changes()}
							title={m.common_unsaved_changes()}
						></span>
					{/if}
				</button>
				<button
					type="button"
					class={cn(
						'hover:bg-foreground/10 mr-1 inline-flex size-5 shrink-0 items-center justify-center rounded opacity-0 group-hover:opacity-100 focus-visible:opacity-100',
						isActive && 'opacity-100'
					)}
					aria-label={m.common_close()}
					onclick={() => onClose(tab.key)}
				>
					<CloseIcon class="size-3" />
				</button>
			</div>
		{/each}
	</div>
	{#if actions}
		<div class="flex shrink-0 items-center gap-0.5 px-1.5">
			{@render actions()}
		</div>
	{/if}
</div>
