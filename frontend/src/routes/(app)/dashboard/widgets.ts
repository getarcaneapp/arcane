import { m } from '#lib/paraglide/messages.js';
import type { DashboardWidgetId, DashboardWidgetLayout } from '#lib/types/auth.js';

export interface DashboardWidget {
	label: () => string;
	minSpan: number;
	maxSpan: number;
	defaultSpan: number;
}

export const DASHBOARD_WIDGETS: Record<DashboardWidgetId, DashboardWidget> = {
	healthy: { label: m.health_status_healthy, minSpan: 2, maxSpan: 12, defaultSpan: 3 },
	attentionCount: { label: m.attention_count, minSpan: 2, maxSpan: 12, defaultSpan: 3 },
	updates: { label: m.updates, minSpan: 2, maxSpan: 12, defaultSpan: 2 },
	risk: { label: m.risk_score, minSpan: 2, maxSpan: 12, defaultSpan: 2 },
	reclaimableTotal: { label: m.reclaimable, minSpan: 2, maxSpan: 12, defaultSpan: 2 },
	environments: { label: m.environments_title, minSpan: 6, maxSpan: 12, defaultSpan: 12 },
	attention: { label: m.needs_attention, minSpan: 4, maxSpan: 12, defaultSpan: 6 },
	activity: { label: m.recent_activity, minSpan: 4, maxSpan: 12, defaultSpan: 6 },
	jobs: { label: m.upcoming_jobs, minSpan: 3, maxSpan: 12, defaultSpan: 4 },
	reclaimable: { label: m.reclaimable_space, minSpan: 3, maxSpan: 12, defaultSpan: 4 },
	memory: { label: m.top_memory, minSpan: 3, maxSpan: 12, defaultSpan: 4 }
};

const DASHBOARD_WIDGET_IDS: DashboardWidgetId[] = [
	'healthy',
	'attentionCount',
	'updates',
	'risk',
	'reclaimableTotal',
	'environments',
	'attention',
	'activity',
	'jobs',
	'reclaimable',
	'memory'
];

export const DEFAULT_DASHBOARD_LAYOUT: DashboardWidgetLayout[] = DASHBOARD_WIDGET_IDS.map((id) => ({
	id,
	span: DASHBOARD_WIDGETS[id].defaultSpan
}));

export const WIDGET_SPAN_CLASSES: Record<number, string> = {
	1: 'lg:col-span-1',
	2: 'lg:col-span-2',
	3: 'lg:col-span-3',
	4: 'lg:col-span-4',
	5: 'lg:col-span-5',
	6: 'lg:col-span-6',
	7: 'lg:col-span-7',
	8: 'lg:col-span-8',
	9: 'lg:col-span-9',
	10: 'lg:col-span-10',
	11: 'lg:col-span-11',
	12: 'lg:col-span-12'
};

export function resolveDashboardLayout(saved: DashboardWidgetLayout[] | undefined): DashboardWidgetLayout[] {
	const known = (saved ?? []).filter((widget) => Object.hasOwn(DASHBOARD_WIDGETS, widget.id));
	if (known.length === 0) return DEFAULT_DASHBOARD_LAYOUT;
	return [...known, ...DEFAULT_DASHBOARD_LAYOUT.filter((widget) => !known.some((other) => other.id === widget.id))].map(
		(widget) => ({
			...widget,
			span: Math.min(Math.max(widget.span, DASHBOARD_WIDGETS[widget.id].minSpan), DASHBOARD_WIDGETS[widget.id].maxSpan)
		})
	);
}

export function placeWidget(
	layout: DashboardWidgetLayout[],
	id: DashboardWidgetId,
	targetId: DashboardWidgetId
): DashboardWidgetLayout[] {
	const moving = layout.find((widget) => widget.id === id);
	const targetIndex = layout.findIndex((widget) => widget.id === targetId);
	if (!moving || targetIndex < 0 || id === targetId) return layout;
	return layout.filter((widget) => widget.id !== id).toSpliced(targetIndex, 0, moving);
}
