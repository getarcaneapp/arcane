import { m } from '#lib/paraglide/messages.js';
import type { DashboardWidgetId, DashboardWidgetLayout } from '#lib/types/auth.js';

export const GRID_COLUMNS = 12;

export interface DashboardWidget {
	label: () => string;
	minW: number;
	minH: number;
	maxH: number;
}

export const DASHBOARD_WIDGETS: Record<DashboardWidgetId, DashboardWidget> = {
	healthy: { label: m.health_status_healthy, minW: 2, minH: 2, maxH: 4 },
	attentionCount: { label: m.attention_count, minW: 2, minH: 2, maxH: 4 },
	updates: { label: m.updates, minW: 2, minH: 2, maxH: 4 },
	risk: { label: m.risk_score, minW: 2, minH: 2, maxH: 4 },
	reclaimableTotal: { label: m.reclaimable, minW: 2, minH: 2, maxH: 4 },
	environments: { label: m.environments_title, minW: 6, minH: 3, maxH: 12 },
	attention: { label: m.needs_attention, minW: 4, minH: 4, maxH: 12 },
	activity: { label: m.recent_activity, minW: 4, minH: 4, maxH: 12 },
	jobs: { label: m.upcoming_jobs, minW: 3, minH: 3, maxH: 12 },
	reclaimable: { label: m.reclaimable_space, minW: 3, minH: 3, maxH: 12 },
	memory: { label: m.top_memory, minW: 3, minH: 3, maxH: 12 }
};

export function defaultDashboardLayout(environmentCount: number): DashboardWidgetLayout[] {
	const tableHeight = Math.min(Math.max(environmentCount + 3, 4), 10);
	const below = 2 + tableHeight;
	return [
		{ id: 'healthy', x: 0, y: 0, w: 3, h: 2 },
		{ id: 'attentionCount', x: 3, y: 0, w: 3, h: 2 },
		{ id: 'updates', x: 6, y: 0, w: 3, h: 2 },
		{ id: 'risk', x: 9, y: 0, w: 3, h: 2 },
		{ id: 'environments', x: 0, y: 2, w: 12, h: tableHeight },
		{ id: 'attention', x: 0, y: below, w: 8, h: 5 },
		{ id: 'activity', x: 0, y: below + 5, w: 8, h: 6 },
		{ id: 'reclaimable', x: 8, y: below, w: 4, h: 3 },
		{ id: 'jobs', x: 8, y: below + 3, w: 4, h: 4 },
		{ id: 'memory', x: 8, y: below + 7, w: 4, h: 4 },
		{ id: 'reclaimableTotal', x: 0, y: below + 11, w: 3, h: 2, hidden: true }
	];
}

function clamp(value: number, low: number, high: number) {
	return Math.min(Math.max(value, low), high);
}

function overlaps(a: DashboardWidgetLayout, b: DashboardWidgetLayout) {
	return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

function clampWidget(widget: DashboardWidgetLayout): DashboardWidgetLayout {
	const { minW, minH, maxH } = DASHBOARD_WIDGETS[widget.id];
	const w = clamp(widget.w || minW, minW, GRID_COLUMNS);
	return {
		...widget,
		x: clamp(widget.x || 0, 0, GRID_COLUMNS - w),
		y: Math.max(widget.y || 0, 0),
		w,
		h: clamp(widget.h || minH, minH, maxH)
	};
}

function settleLayout(layout: DashboardWidgetLayout[], pinnedId?: DashboardWidgetId): DashboardWidgetLayout[] {
	const pinned = layout.find((widget) => widget.id === pinnedId && !widget.hidden);
	const placed: DashboardWidgetLayout[] = pinned ? [pinned] : [];
	for (const widget of layout.filter((entry) => !entry.hidden && entry !== pinned).toSorted((a, b) => a.y - b.y || a.x - b.x)) {
		let y = widget.y;
		let blockers = placed.filter((other) => overlaps({ ...widget, y }, other));
		while (blockers.length > 0) {
			y = Math.max(...blockers.map((other) => other.y + other.h));
			blockers = placed.filter((other) => overlaps({ ...widget, y }, other));
		}
		placed.push({ ...widget, y });
	}
	return layout.map((widget) => placed.find((entry) => entry.id === widget.id) ?? widget);
}

export function moveWidget(
	layout: DashboardWidgetLayout[],
	id: DashboardWidgetId,
	changes: Partial<Pick<DashboardWidgetLayout, 'x' | 'y' | 'w' | 'h'>>
): DashboardWidgetLayout[] {
	return settleLayout(
		layout.map((widget) => (widget.id === id ? clampWidget({ ...widget, ...changes }) : widget)),
		id
	);
}

export function showWidget(layout: DashboardWidgetLayout[], id: DashboardWidgetId): DashboardWidgetLayout[] {
	const widget = layout.find((entry) => entry.id === id);
	if (!widget) return layout;
	const visible = layout.filter((entry) => !entry.hidden && entry.id !== id);
	for (let y = 0; ; y++) {
		for (let x = 0; x <= GRID_COLUMNS - widget.w; x++) {
			if (!visible.some((other) => overlaps({ ...widget, x, y }, other))) {
				return layout.map((entry) => (entry.id === id ? { ...entry, x, y, hidden: false } : entry));
			}
		}
	}
}

export function resolveDashboardLayout(
	saved: DashboardWidgetLayout[] | undefined,
	environmentCount: number
): DashboardWidgetLayout[] {
	const defaults = defaultDashboardLayout(environmentCount);
	const known = (saved ?? []).filter((widget) => Object.hasOwn(DASHBOARD_WIDGETS, widget.id)).map(clampWidget);
	if (known.length === 0) return defaults;
	return defaults
		.filter((widget) => !known.some((other) => other.id === widget.id))
		.reduce(
			(layout, widget) => (widget.hidden ? [...layout, widget] : showWidget([...layout, { ...widget, hidden: true }], widget.id)),
			settleLayout(known)
		);
}
