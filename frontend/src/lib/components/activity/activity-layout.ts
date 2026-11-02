import { PersistedState } from 'runed';

import { m } from '#lib/paraglide/messages.js';

export type ActivityCenterLayout = 'tray' | 'inspector';

export const activityCenterLayout = new PersistedState<ActivityCenterLayout>('arcane-activity-layout', 'tray');

export const activityCenterLayouts: { value: ActivityCenterLayout; label: () => string }[] = [
	{ value: 'tray', label: m.activity_layout_tray },
	{ value: 'inspector', label: m.activity_layout_inspector }
];

export function isActivityCenterLayout(value: string): value is ActivityCenterLayout {
	return activityCenterLayouts.some((layout) => layout.value === value);
}
