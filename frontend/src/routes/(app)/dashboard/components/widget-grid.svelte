<script lang="ts">
	import { tick, type Snippet } from 'svelte';
	import { flip } from 'svelte/animate';

	import { EyeOffIcon, GripIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { DashboardWidgetId, DashboardWidgetLayout } from '#lib/types/auth.js';
	import { cn } from '#lib/utils.js';

	import { DASHBOARD_WIDGETS, placeWidget, WIDGET_SPAN_CLASSES } from '../widgets';

	const GRID_COLUMNS = 12;
	const EDIT_INSET = 12;
	const KEY_OFFSETS: Record<string, number> = { ArrowLeft: -1, ArrowUp: -1, ArrowRight: 1, ArrowDown: 1 };
	const CONTROL_CLASS =
		'inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-40';

	let {
		layout,
		editing,
		onChange,
		widget
	}: {
		layout: DashboardWidgetLayout[];
		editing: boolean;
		onChange: (layout: DashboardWidgetLayout[]) => void;
		widget: Snippet<[DashboardWidgetId]>;
	} = $props();

	let grid = $state<HTMLElement | null>(null);
	let dragging = $state<{ id: DashboardWidgetId; overId: DashboardWidgetId | null } | null>(null);
	let resizing = $state<{
		id: DashboardWidgetId;
		startX: number;
		startSpan: number;
		span: number;
		columnWidth: number;
		gap: number;
	} | null>(null);

	const visible = $derived(layout.filter((item) => !item.hidden));
	let heights = $state.raw<Partial<Record<DashboardWidgetId, number>>>({});

	$effect.pre(() => {
		if (!editing || !grid) return;
		heights = Object.fromEntries(
			Array.from(grid.querySelectorAll<HTMLElement>('[data-widget-id]'), (element) => [
				element.dataset['widgetId'],
				element.offsetHeight
			])
		);
	});

	function editZoom(id: DashboardWidgetId) {
		const height = heights[id];
		if (!height) return 0.9;
		return Math.min(Math.max((height - EDIT_INSET * 2) / height, 0.75), 0.95);
	}

	function resize(id: DashboardWidgetId, span: number) {
		const { minSpan, maxSpan } = DASHBOARD_WIDGETS[id];
		onChange(layout.map((item) => (item.id === id ? { ...item, span: Math.min(Math.max(span, minSpan), maxSpan) } : item)));
	}

	async function moveBy(id: DashboardWidgetId, offset: number) {
		const target = visible[visible.findIndex((item) => item.id === id) + offset];
		if (!target) return;
		onChange(placeWidget(layout, id, target.id));
		await tick();
		grid?.querySelector<HTMLElement>(`[data-widget-handle="${id}"]`)?.focus();
	}

	function startDrag(event: PointerEvent, id: DashboardWidgetId) {
		if (event.button !== 0) return;
		event.preventDefault();
		dragging = { id, overId: null };
	}

	function drag(event: PointerEvent) {
		if (!dragging) return;
		const hovered = document.elementFromPoint(event.clientX, event.clientY)?.closest<HTMLElement>('[data-widget-id]');
		const overId = visible.find((item) => item.id === hovered?.dataset['widgetId'] && item.id !== dragging?.id)?.id ?? null;
		if (overId !== dragging.overId) dragging = { id: dragging.id, overId };
	}

	function drop() {
		if (dragging?.overId) onChange(placeWidget(layout, dragging.id, dragging.overId));
		dragging = null;
	}

	function startResize(event: PointerEvent, item: DashboardWidgetLayout) {
		if (event.button !== 0) return;
		event.preventDefault();
		if (!grid) return;
		const gap = Number.parseFloat(getComputedStyle(grid).columnGap) || 0;
		resizing = {
			id: item.id,
			startX: event.clientX,
			startSpan: item.span,
			span: item.span,
			columnWidth: (grid.clientWidth + gap) / GRID_COLUMNS,
			gap
		};
	}

	function resizeTo(event: PointerEvent) {
		if (!resizing) return;
		const { minSpan, maxSpan } = DASHBOARD_WIDGETS[resizing.id];
		const span = Math.min(
			Math.max(resizing.startSpan + Math.round((event.clientX - resizing.startX) / resizing.columnWidth), minSpan),
			maxSpan
		);
		if (span !== resizing.span) resizing = { ...resizing, span };
	}

	function endPointer() {
		drop();
		if (resizing && resizing.span !== resizing.startSpan) resize(resizing.id, resizing.span);
		resizing = null;
	}

	function moveWithKeyboard(event: KeyboardEvent, id: DashboardWidgetId) {
		const offset = KEY_OFFSETS[event.key];
		if (offset === undefined) return;
		event.preventDefault();
		void moveBy(id, offset);
	}
</script>

<svelte:window
	onpointermove={(event) => {
		drag(event);
		resizeTo(event);
	}}
	onpointerup={endPointer}
	onpointercancel={() => {
		dragging = null;
		resizing = null;
	}}
/>

<div bind:this={grid} class={cn('grid grid-cols-1 gap-4 lg:grid-cols-12', (dragging || resizing) && 'select-none')}>
	{#each visible as item (item.id)}
		{@const definition = DASHBOARD_WIDGETS[item.id]}
		{@const name = definition.label()}
		<div
			data-widget-id={item.id}
			animate:flip={{ duration: 150 }}
			class={cn(
				'relative flex min-w-0 flex-col',
				WIDGET_SPAN_CLASSES[item.span],
				editing && 'rounded-xl p-3 outline-2 outline-border/70 transition-colors outline-dashed',
				dragging?.id === item.id && 'opacity-50',
				dragging?.overId === item.id && 'bg-primary/5 outline-primary'
			)}
		>
			{#if editing}
				<div class="pointer-events-none absolute inset-x-4 -top-3.5 z-10 flex justify-center">
					<div
						class="pointer-events-auto flex h-7 min-w-0 items-center gap-0.5 rounded-full border border-border/70 bg-background pr-0.5 pl-3 shadow-sm"
					>
						<span class="mr-1 min-w-0 truncate text-xs font-medium text-muted-foreground">{name}</span>
						<button
							type="button"
							data-widget-handle={item.id}
							class={cn(CONTROL_CLASS, 'size-6 shrink-0 cursor-grab touch-none rounded-full active:cursor-grabbing')}
							aria-label={m.reorder_name({ name })}
							title={m.reorder_name({ name })}
							onpointerdown={(event) => startDrag(event, item.id)}
							onkeydown={(event) => moveWithKeyboard(event, item.id)}
						>
							<GripIcon class="size-3.5" />
						</button>
						<button
							type="button"
							class={cn(CONTROL_CLASS, 'size-6 shrink-0 rounded-full')}
							aria-label={m.hide_name({ name })}
							title={m.hide_name({ name })}
							onclick={() => onChange(layout.map((entry) => (entry.id === item.id ? { ...entry, hidden: true } : entry)))}
						>
							<EyeOffIcon class="size-3.5" />
						</button>
					</div>
				</div>
			{/if}
			<div
				inert={editing}
				class={cn('grid min-w-0 flex-1', editing && 'pointer-events-none select-none')}
				style:zoom={editing ? editZoom(item.id) : undefined}
			>
				{@render widget(item.id)}
			</div>
			{#if resizing?.id === item.id}
				<div
					class="pointer-events-none absolute inset-y-0 left-0 z-20 rounded-xl bg-primary/10 outline-2 outline-primary transition-all duration-100 ease-out outline-dashed"
					style:width="{resizing.span * resizing.columnWidth - resizing.gap}px"
				>
					<span
						class="absolute right-2 bottom-2 rounded-md bg-primary px-1.5 py-0.5 text-xs font-medium text-primary-foreground tabular-nums"
					>
						{resizing.span}/{GRID_COLUMNS}
					</span>
				</div>
			{/if}
			{#if editing}
				<button
					type="button"
					aria-label={m.resize_name({ name })}
					title={m.resize_name({ name })}
					class="group absolute -right-0.5 -bottom-0.5 hidden size-6 cursor-nwse-resize touch-none items-end justify-end rounded-md p-1.5 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none lg:flex"
					onpointerdown={(event) => startResize(event, item)}
					onkeydown={(event) => {
						const offset = KEY_OFFSETS[event.key];
						if (offset === undefined) return;
						event.preventDefault();
						resize(item.id, item.span + offset);
					}}
				>
					<span
						class={cn(
							'size-3 rounded-br-md border-r-2 border-b-2 border-muted-foreground/70 transition-colors group-hover:border-primary group-focus-visible:border-primary',
							resizing?.id === item.id && 'border-primary'
						)}
					></span>
				</button>
			{/if}
		</div>
	{/each}
</div>
