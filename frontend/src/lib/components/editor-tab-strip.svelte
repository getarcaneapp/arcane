<script lang="ts">
	import type { Snippet } from 'svelte';

	import { setEditorStatusHost, type EditorStatusHost } from '#lib/components/code-editor/status-context.js';
	import { CloseIcon, FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

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
		children?: Snippet;
	}

	let { tabs, activeKey, onSelect, onClose, actions, children }: Props = $props();

	const statusHost = $state<EditorStatusHost>({ current: null });
	setEditorStatusHost(statusHost);
	const status = $derived(statusHost.current?.());

	// Middle-click anywhere on a tab closes it like browser and IDE tabs; preventing mousedown stops Windows auto-scroll.
	function preventMiddleMouseDown(event: MouseEvent) {
		if (event.button === 1) event.preventDefault();
	}

	function closeOnMiddleClick(event: MouseEvent, key: string) {
		if (event.button !== 1) return;
		event.preventDefault();
		onClose(key);
	}
</script>

{#snippet editorStatus(className: string)}
	{#if status}
		<div class={cn('items-center gap-3 text-2xs whitespace-nowrap text-muted-foreground', className)}>
			<span>{status.errors} {m.editor_errors()}</span>
			<span>{status.warnings} {m.editor_warnings()}</span>
			<span>{m.editor_schema()}: {status.schemaStatus}</span>
			<span>{m.editor_line()} {status.cursorLine}, {m.editor_column()} {status.cursorCol}</span>
			<span>{m.diagnostics()}: {status.diagnostics}</span>
			{#if status.validating}
				<span class="opacity-70">{m.editor_validating()}</span>
			{/if}
		</div>
	{/if}
{/snippet}

<div
	class="@container flex h-full min-h-0 flex-1 flex-col overflow-hidden rounded-xl bg-table-head"
	style="--code-editor-background: transparent; --code-editor-gutter-background: var(--table-head)"
>
	{#if tabs.length > 0 || actions || status}
		<div class="flex h-10 shrink-0 items-center gap-2 px-1.5">
			<div class="scrollbar-hide flex h-full min-w-0 flex-1 items-center gap-1 overflow-x-auto">
				{#each tabs as tab (tab.key)}
					{@const isActive = activeKey === tab.key}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div
						class={cn(
							'group flex h-7 shrink-0 items-center rounded-lg transition-colors',
							isActive ? 'bg-foreground/8' : 'hover:bg-foreground/4'
						)}
						data-tab-key={tab.key}
						data-active={isActive}
						onmousedown={preventMiddleMouseDown}
						onauxclick={(event) => closeOnMiddleClick(event, tab.key)}
					>
						<button
							type="button"
							class={cn(
								'flex h-full items-center gap-1.5 pr-1 pl-2.5 text-xs-plus',
								isActive ? 'text-foreground' : 'text-muted-foreground'
							)}
							title={tab.title}
							onclick={() => onSelect(tab.key)}
						>
							<FileTextIcon class={cn('size-3.5 shrink-0', tab.iconClass)} />
							<span class="max-w-40 truncate">{tab.label}</span>
							{#if tab.pending}
								<span
									class="size-1.5 shrink-0 rounded-full bg-primary"
									role="img"
									aria-label={m.common_unsaved_changes()}
									title={m.common_unsaved_changes()}
								></span>
							{/if}
						</button>
						<button
							type="button"
							class={cn(
								'mr-1 inline-flex size-5 shrink-0 items-center justify-center rounded-md opacity-0 group-hover:opacity-100 hover:bg-foreground/10 focus-visible:opacity-100 pointer-coarse:opacity-100',
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
			{@render editorStatus('hidden shrink-0 @3xl:flex')}
			{#if actions}
				<div class="flex shrink-0 items-center gap-0.5">
					{@render actions()}
				</div>
			{/if}
		</div>
	{/if}
	{#if children}
		<div class="flex min-h-0 flex-1 flex-col">
			{@render children()}
		</div>
		{@render editorStatus('flex shrink-0 overflow-x-auto px-3 py-1 @3xl:hidden')}
	{/if}
</div>
