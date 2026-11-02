<script lang="ts">
	import { tick, type Snippet } from 'svelte';

	import { EyeOffIcon, SettingsIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';
	import { DASHBOARD_WIDGET_TYPES, type DashboardWidget, GRID_COLUMNS, moveWidget } from '#lib/utils/dashboard-layout.js';

	const ROW_PITCH_REM = 4;
	const KEY_STEPS: Record<string, { x: number; y: number }> = {
		ArrowLeft: { x: -1, y: 0 },
		ArrowRight: { x: 1, y: 0 },
		ArrowUp: { x: 0, y: -1 },
		ArrowDown: { x: 0, y: 1 }
	};
	const CONTROL_CLASS =
		'inline-flex size-6 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none';

	let {
		layout,
		editing,
		onChange,
		onConfigure,
		widget
	}: {
		layout: DashboardWidget[];
		editing: boolean;
		onChange: (layout: DashboardWidget[]) => void;
		onConfigure: (widget: DashboardWidget) => void;
		widget: Snippet<[DashboardWidget]>;
	} = $props();

	let grid = $state<HTMLElement | null>(null);
	let pointer = $state<{
		mode: 'move' | 'resize';
		origin: DashboardWidget;
		startX: number;
		startY: number;
		dx: number;
		dy: number;
		columnPitch: number;
		rowPitch: number;
	} | null>(null);

	const preview = $derived.by(() => {
		if (!pointer) return layout;
		const { mode, origin, dx, dy, columnPitch, rowPitch } = pointer;
		const columns = Math.round(dx / columnPitch);
		const rows = Math.round(dy / rowPitch);
		return moveWidget(
			layout,
			origin.id,
			mode === 'move' ? { x: origin.x + columns, y: origin.y + rows } : { w: origin.w + columns, h: origin.h + rows }
		);
	});
	const positions = $derived(new Map(preview.map((item) => [item.id, item])));
	const ordered = $derived(layout.filter((item) => !item.hidden).toSorted((a, b) => a.y - b.y || a.x - b.x));
	const rows = $derived(Math.max(0, ...preview.filter((item) => !item.hidden).map((item) => item.y + item.h)));
	const target = $derived(pointer ? positions.get(pointer.origin.id) : undefined);

	function start(event: PointerEvent, item: DashboardWidget, mode: 'move' | 'resize') {
		if (event.button !== 0 || !grid || !window.matchMedia('(width >= 64rem)').matches) return;
		event.preventDefault();
		const rem = Number.parseFloat(getComputedStyle(document.documentElement).fontSize);
		pointer = {
			mode,
			origin: item,
			startX: event.clientX,
			startY: event.clientY,
			dx: 0,
			dy: 0,
			columnPitch: (grid.clientWidth + rem) / GRID_COLUMNS,
			rowPitch: rem * ROW_PITCH_REM
		};
	}

	function endPointer() {
		if (!pointer) return;
		const next = preview;
		pointer = null;
		onChange(next);
	}

	async function step(event: KeyboardEvent, item: DashboardWidget, mode: 'move' | 'resize') {
		const delta = KEY_STEPS[event.key];
		if (!delta) return;
		event.preventDefault();
		onChange(
			moveWidget(
				layout,
				item.id,
				mode === 'move' ? { x: item.x + delta.x, y: item.y + delta.y } : { w: item.w + delta.x, h: item.h + delta.y }
			)
		);
		await tick();
		grid?.querySelector<HTMLElement>(`[data-widget-${mode}="${item.id}"]`)?.focus();
	}
</script>

<svelte:window
	onpointermove={(event) => {
		if (pointer) pointer = { ...pointer, dx: event.clientX - pointer.startX, dy: event.clientY - pointer.startY };
	}}
	onpointerup={endPointer}
	onpointercancel={() => (pointer = null)}
/>

<div
	bind:this={grid}
	class={cn(
		'dashboard-grid',
		pointer && 'select-none',
		pointer?.mode === 'move' && 'cursor-grabbing',
		pointer?.mode === 'resize' && 'cursor-nwse-resize'
	)}
	style:--grid-rows={rows}
>
	{#if pointer?.mode === 'move' && target}
		<div
			class="pointer-events-none dashboard-widget rounded-xl bg-primary/10 outline-2 outline-primary outline-dashed max-lg:hidden"
			style:--widget-x={target.x}
			style:--widget-y={target.y}
			style:--widget-w={target.w}
			style:--widget-h={target.h}
		></div>
	{/if}
	{#each ordered as item (item.id)}
		{@const name = item.title || DASHBOARD_WIDGET_TYPES[item.type].label()}
		{@const cell = positions.get(item.id) ?? item}
		{@const active = pointer?.origin.id === item.id}
		{@const moving = active && pointer?.mode === 'move'}
		<div
			data-widget-id={item.id}
			data-moving={moving || undefined}
			class={cn(
				'dashboard-widget flex min-w-0 flex-col max-lg:relative',
				editing && 'rounded-xl p-3 outline-2 outline-border/70 outline-dashed',
				active && 'outline-primary',
				moving && 'z-30 bg-background/80 shadow-xl'
			)}
			style:--widget-x={cell.x}
			style:--widget-y={cell.y}
			style:--widget-w={cell.w}
			style:--widget-h={cell.h}
			style:--widget-dx={moving && pointer ? `${(pointer.origin.x - cell.x) * pointer.columnPitch + pointer.dx}px` : undefined}
			style:--widget-dy={moving && pointer ? `${(pointer.origin.y - cell.y) * pointer.rowPitch + pointer.dy}px` : undefined}
		>
			<div
				inert={editing}
				class={cn('grid min-h-0 min-w-0', editing ? 'pointer-events-none widget-scaled shrink-0 select-none' : 'flex-1')}
			>
				{@render widget(item)}
			</div>
			{#if editing}
				<button
					type="button"
					data-widget-move={item.id}
					class={cn(
						'absolute inset-0 z-10 rounded-xl focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none lg:cursor-grab lg:touch-none',
						moving && 'lg:cursor-grabbing'
					)}
					aria-label={m.move_name({ name })}
					title={m.move_name({ name })}
					onpointerdown={(event) => start(event, item, 'move')}
					onkeydown={(event) => step(event, item, 'move')}
				></button>
				<div class="pointer-events-none absolute inset-x-4 -top-3.5 z-20 flex justify-center">
					<div
						class="pointer-events-auto flex h-7 min-w-0 items-center gap-0.5 rounded-full border border-border/70 bg-background pr-0.5 pl-3 shadow-sm"
					>
						<span class="mr-1 min-w-0 truncate text-xs font-medium text-muted-foreground">{name}</span>
						{#if DASHBOARD_WIDGET_TYPES[item.type].multi}
							<button
								type="button"
								class={CONTROL_CLASS}
								aria-label={m.configure_name({ name })}
								title={m.configure_name({ name })}
								onclick={() => onConfigure(item)}
							>
								<SettingsIcon class="size-3.5" />
							</button>
							<button
								type="button"
								class={CONTROL_CLASS}
								aria-label={m.remove_name({ name })}
								title={m.remove_name({ name })}
								onclick={() => onChange(layout.filter((entry) => entry.id !== item.id))}
							>
								<TrashIcon class="size-3.5" />
							</button>
						{:else}
							<button
								type="button"
								class={CONTROL_CLASS}
								aria-label={m.hide_name({ name })}
								title={m.hide_name({ name })}
								onclick={() => onChange(layout.map((entry) => (entry.id === item.id ? { ...entry, hidden: true } : entry)))}
							>
								<EyeOffIcon class="size-3.5" />
							</button>
						{/if}
					</div>
				</div>
				{#if active && pointer?.mode === 'resize'}
					<span
						class="pointer-events-none absolute right-8 bottom-2 z-20 rounded-md bg-primary px-1.5 py-0.5 text-xs font-medium text-primary-foreground tabular-nums"
					>
						{cell.w} × {cell.h}
					</span>
				{/if}
				<button
					type="button"
					data-widget-resize={item.id}
					aria-label={m.resize_name({ name })}
					title={m.resize_name({ name })}
					class="group absolute -right-0.5 -bottom-0.5 z-20 hidden size-6 cursor-nwse-resize touch-none items-end justify-end rounded-md p-1.5 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none lg:flex"
					onpointerdown={(event) => start(event, item, 'resize')}
					onkeydown={(event) => step(event, item, 'resize')}
				>
					<span
						class={cn(
							'size-3 rounded-br-md border-r-2 border-b-2 border-muted-foreground/70 transition-colors group-hover:border-primary group-focus-visible:border-primary',
							active && pointer?.mode === 'resize' && 'border-primary'
						)}
					></span>
				</button>
			{/if}
		</div>
	{/each}
</div>
