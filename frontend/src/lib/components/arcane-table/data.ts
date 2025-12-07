import BadgeCheckIcon from 'phosphor-svelte/lib/SealCheck';
import BadgeXIcon from 'phosphor-svelte/lib/Seal';
import CircleFadingArrowUp from 'phosphor-svelte/lib/ArrowCircleUp';
import CircleCheck from 'phosphor-svelte/lib/CheckCircle';
import InfoIcon from 'phosphor-svelte/lib/Info';
import TriangleAlertIcon from 'phosphor-svelte/lib/WarningCircle';
import CircleXIcon from 'phosphor-svelte/lib/XCircle';
import CircleCheckIcon from 'phosphor-svelte/lib/CheckCircle';
import FolderOpenIcon from 'phosphor-svelte/lib/FolderOpen';
import GlobeIcon from 'phosphor-svelte/lib/Globe';
import { m } from '$lib/paraglide/messages';

export const usageFilters = [
	{
		value: true,
		label: m.common_in_use(),
		icon: BadgeCheckIcon
	},
	{
		value: false,
		label: m.common_unused(),
		icon: CircleCheck
	}
];

export const imageUpdateFilters = [
	{
		value: true,
		label: m.images_has_updates(),
		icon: CircleFadingArrowUp
	},
	{
		value: false,
		label: m.images_no_updates(),
		icon: BadgeXIcon
	}
];

export const severityFilters = [
	{
		value: 'info',
		label: m.events_info(),
		icon: InfoIcon
	},
	{
		value: 'success',
		label: m.events_success(),
		icon: CircleCheckIcon
	},
	{
		value: 'warning',
		label: m.events_warning(),
		icon: TriangleAlertIcon
	},
	{
		value: 'error',
		label: m.events_error(),
		icon: CircleXIcon
	}
];

export const templateTypeFilters = [
	{
		value: 'false',
		label: m.templates_local(),
		icon: FolderOpenIcon
	},
	{
		value: 'true',
		label: m.templates_remote(),
		icon: GlobeIcon
	}
];
