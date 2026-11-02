import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import { toast } from 'svelte-sonner';

import type { ActionButton } from '#lib/components/action-button-group/types.js';
import { PinOnIcon } from '#lib/icons/index.js';
import { m } from '#lib/paraglide/messages.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import type {
	DashboardResourceKind,
	DashboardWidgetConfig,
	DashboardWidgetLayout,
	DashboardWidgetType
} from '#lib/types/auth.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { ENVIRONMENT_FILTER, environmentFilterIds } from '#lib/utils/environments.js';
import { saveUserPreferences } from '#lib/utils/user-preferences.js';

export const GRID_COLUMNS = 12;

export const MAX_DASHBOARD_WIDGETS = 50;

export interface DashboardWidgetDefinition {
	label: () => string;
	minW: number;
	minH: number;
	maxH: number;
	multi: boolean;
	defaultW: number;
	defaultH: number;
}

export type DashboardWidgetInput = DashboardWidgetLayout & { type: DashboardWidgetType };

export type DashboardWidget = DashboardWidgetInput & { id: string };

export const DASHBOARD_WIDGET_TYPES: Record<DashboardWidgetType, DashboardWidgetDefinition> = {
	healthy: { label: m.health_status_healthy, minW: 2, minH: 2, maxH: 4, multi: false, defaultW: 3, defaultH: 2 },
	attentionCount: { label: m.attention_count, minW: 2, minH: 2, maxH: 4, multi: false, defaultW: 3, defaultH: 2 },
	updates: { label: m.updates, minW: 2, minH: 2, maxH: 4, multi: false, defaultW: 3, defaultH: 2 },
	risk: { label: m.risk_score, minW: 2, minH: 2, maxH: 4, multi: false, defaultW: 3, defaultH: 2 },
	reclaimableTotal: { label: m.reclaimable, minW: 2, minH: 2, maxH: 4, multi: false, defaultW: 3, defaultH: 2 },
	environments: { label: m.environments_title, minW: 6, minH: 3, maxH: 12, multi: false, defaultW: 12, defaultH: 6 },
	attention: { label: m.needs_attention, minW: 4, minH: 4, maxH: 12, multi: false, defaultW: 8, defaultH: 5 },
	activity: { label: m.recent_activity, minW: 4, minH: 4, maxH: 12, multi: false, defaultW: 8, defaultH: 6 },
	jobs: { label: m.upcoming_jobs, minW: 3, minH: 3, maxH: 12, multi: false, defaultW: 4, defaultH: 4 },
	reclaimable: { label: m.reclaimable_space, minW: 3, minH: 3, maxH: 12, multi: false, defaultW: 4, defaultH: 3 },
	memory: { label: m.top_memory, minW: 3, minH: 3, maxH: 12, multi: false, defaultW: 4, defaultH: 4 },
	resourceQuery: { label: m.filtered_resources, minW: 2, minH: 2, maxH: 12, multi: true, defaultW: 4, defaultH: 4 },
	resource: { label: m.resource, minW: 3, minH: 2, maxH: 6, multi: true, defaultW: 4, defaultH: 3 },
	containerStats: { label: m.container_stats, minW: 4, minH: 5, maxH: 12, multi: true, defaultW: 6, defaultH: 7 },
	containerLogs: { label: m.containers_logs_title, minW: 4, minH: 4, maxH: 12, multi: true, defaultW: 6, defaultH: 6 },
	environment: { label: m.resource_environment_cap, minW: 3, minH: 3, maxH: 6, multi: true, defaultW: 4, defaultH: 4 }
};

const WIDGET_TYPES = Object.keys(DASHBOARD_WIDGET_TYPES) as DashboardWidgetType[];

export function defaultDashboardLayout(environmentCount: number): DashboardWidget[] {
	const tableHeight = Math.min(Math.max(environmentCount + 3, 4), 10);
	const below = 2 + tableHeight;
	return [
		{ id: 'healthy', type: 'healthy', x: 0, y: 0, w: 3, h: 2 },
		{ id: 'attentionCount', type: 'attentionCount', x: 3, y: 0, w: 3, h: 2 },
		{ id: 'updates', type: 'updates', x: 6, y: 0, w: 3, h: 2 },
		{ id: 'risk', type: 'risk', x: 9, y: 0, w: 3, h: 2 },
		{ id: 'environments', type: 'environments', x: 0, y: 2, w: 12, h: tableHeight },
		{ id: 'attention', type: 'attention', x: 0, y: below, w: 8, h: 5 },
		{ id: 'activity', type: 'activity', x: 0, y: below + 5, w: 8, h: 6 },
		{ id: 'reclaimable', type: 'reclaimable', x: 8, y: below, w: 4, h: 3 },
		{ id: 'jobs', type: 'jobs', x: 8, y: below + 3, w: 4, h: 4 },
		{ id: 'memory', type: 'memory', x: 8, y: below + 7, w: 4, h: 4 },
		{ id: 'reclaimableTotal', type: 'reclaimableTotal', x: 0, y: below + 11, w: 3, h: 2, hidden: true }
	];
}

function clamp(value: number, low: number, high: number) {
	return Math.min(Math.max(value, low), high);
}

function overlaps(a: DashboardWidgetLayout, b: DashboardWidgetLayout) {
	return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

function clampWidget<T extends DashboardWidgetInput>(widget: T): T {
	const { minW, minH, maxH } = DASHBOARD_WIDGET_TYPES[widget.type];
	const w = clamp(widget.w || minW, minW, GRID_COLUMNS);
	return {
		...widget,
		x: clamp(widget.x || 0, 0, GRID_COLUMNS - w),
		y: Math.max(widget.y || 0, 0),
		w,
		h: clamp(widget.h || minH, minH, maxH)
	};
}

function settleLayout(layout: DashboardWidget[], pinnedId?: string): DashboardWidget[] {
	const pinned = layout.find((widget) => widget.id === pinnedId && !widget.hidden);
	const placed: DashboardWidget[] = pinned ? [pinned] : [];
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
	layout: DashboardWidget[],
	id: string,
	changes: Partial<Pick<DashboardWidgetLayout, 'x' | 'y' | 'w' | 'h'>>
): DashboardWidget[] {
	return settleLayout(
		layout.map((widget) => (widget.id === id ? clampWidget({ ...widget, ...changes }) : widget)),
		id
	);
}

function freeSpot(layout: DashboardWidgetLayout[], widget: DashboardWidgetLayout) {
	const visible = layout.filter((entry) => !entry.hidden);
	for (let y = 0; ; y++) {
		for (let x = 0; x <= GRID_COLUMNS - widget.w; x++) {
			if (!visible.some((other) => overlaps({ ...widget, x, y }, other))) return { x, y };
		}
	}
}

export function showWidget(layout: DashboardWidget[], id: string): DashboardWidget[] {
	const widget = layout.find((entry) => entry.id === id);
	if (!widget) return layout;
	const spot = freeSpot(
		layout.filter((entry) => entry.id !== id),
		widget
	);
	return layout.map((entry) => (entry.id === id ? { ...entry, ...spot, hidden: false } : entry));
}

export function addDashboardWidget(layout: DashboardWidgetLayout[], instance: DashboardWidgetInput): DashboardWidgetLayout[] {
	if (
		layout.length >= MAX_DASHBOARD_WIDGETS ||
		(!DASHBOARD_WIDGET_TYPES[instance.type].multi && layout.some((widget) => widget.type === instance.type))
	) {
		return layout;
	}
	const widget = clampWidget(instance);
	return [...layout, { ...widget, ...freeSpot(layout, widget), hidden: false }];
}

export function createDashboardWidget(
	type: DashboardWidgetType,
	config?: DashboardWidgetConfig,
	title?: string
): DashboardWidgetInput {
	const { defaultW, defaultH } = DASHBOARD_WIDGET_TYPES[type];
	return { type, title, x: 0, y: 0, w: defaultW, h: defaultH, config };
}

export function resolveDashboardLayout(saved: DashboardWidgetLayout[] | undefined, environmentCount: number): DashboardWidget[] {
	const defaults = defaultDashboardLayout(environmentCount);
	const known = (saved ?? [])
		.flatMap((widget) => {
			const { id } = widget;
			const type = WIDGET_TYPES.find((key) => key === (widget.type ?? id));
			return type && id ? [clampWidget({ ...widget, id, type })] : [];
		})
		.filter(
			(widget, index, all) =>
				all.findIndex(
					(other) => other.id === widget.id || (!DASHBOARD_WIDGET_TYPES[widget.type].multi && other.type === widget.type)
				) === index
		);
	if (known.length === 0) return defaults;
	return defaults
		.filter((widget) => !known.some((other) => other.type === widget.type))
		.reduce(
			(layout, widget) => (widget.hidden ? [...layout, widget] : showWidget([...layout, { ...widget, hidden: true }], widget.id)),
			settleLayout(known)
		);
}

export function pinDashboardWidget(instance: DashboardWidgetInput): boolean {
	const saved = userStore.current?.preferences?.dashboardLayout ?? [];
	const layout = [
		...resolveDashboardLayout(saved, environmentStore.available.filter((environment) => environment.enabled).length),
		...saved.filter((widget) => !widget.id)
	];
	const next = addDashboardWidget(layout, instance);
	if (next === layout) {
		toast.error(m.widget_limit_reached({ max: MAX_DASHBOARD_WIDGETS }));
		return false;
	}
	void saveUserPreferences({ dashboardLayout: next });
	toast.success(m.pinned_to_dashboard(), {
		action: {
			label: m.error_go_to_dashboard(),
			onClick: () => goto(resolve('/(app)/dashboard'))
		}
	});
	return true;
}

export function pinDashboardAction(id: string, label: string, widget: () => DashboardWidgetInput): ActionButton {
	return {
		id,
		action: 'base',
		label,
		icon: PinOnIcon,
		group: 'manage',
		onclick: () => pinDashboardWidget(widget())
	};
}

export function pinViewAction(
	resourceKind: DashboardResourceKind,
	request: SearchPaginationSortRequest,
	environments: EnvironmentRef[]
): ActionButton {
	return pinDashboardAction('pin-view', m.pin_this_view(), () => {
		const { [ENVIRONMENT_FILTER]: environmentFilter, ...filters } = request.filters ?? {};
		const picked = environmentFilterIds(environmentFilter, environments);
		const entries = Object.entries(filters).map(([key, value]): [string, string] => [
			key,
			Array.isArray(value) ? value.join(',') : String(value)
		]);
		return createDashboardWidget('resourceQuery', {
			resourceKind,
			environmentIds: picked.length > 0 ? picked : environments.map((environment) => environment.id),
			search: request.search || undefined,
			filters: entries.length > 0 ? Object.fromEntries(entries) : undefined,
			display: 'list',
			limit: 10
		});
	});
}
