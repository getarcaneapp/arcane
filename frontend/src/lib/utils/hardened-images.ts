import { ShieldAlertIcon, ShieldCheckIcon, ShieldXIcon, type IconType } from '#lib/icons/index.js';
import { m } from '#lib/paraglide/messages.js';
import type { HardenedImageInfo } from '#lib/types/image.js';
import { formatRelativeTime } from '#lib/utils/formatting.js';

export interface HardenedImageDisplay {
	label: string;
	variant: 'green' | 'amber' | 'red';
	icon: IconType;
}

export function hardenedImageDisplay(info: HardenedImageInfo): HardenedImageDisplay {
	switch (info.supportStatus) {
		case 'end_of_life':
			return { label: m.end_of_life(), variant: 'red', icon: ShieldXIcon };
		case 'ending_soon':
			return { label: m.ending_soon(), variant: 'amber', icon: ShieldAlertIcon };
		default:
			return { label: m.hardened(), variant: 'green', icon: ShieldCheckIcon };
	}
}

export function hardenedSupportSummary(info: HardenedImageInfo): string {
	if (!info.endOfLifeDate) return '';
	const time = formatRelativeTime(info.endOfLifeDate);
	return info.supportStatus === 'end_of_life' ? m.support_ended_time({ time }) : m.support_ends_time({ time });
}
