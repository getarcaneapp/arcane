import { m } from '$lib/paraglide/messages';
import { GlobeIcon, FolderOpenIcon, VerifiedCheckIcon, AlertIcon, InfoIcon, CloseIcon, CheckIcon, UpdateIcon } from '$lib/icons';

export const usageFilters = [
	{
		value: true,
		label: m.common_in_use(),
		icon: VerifiedCheckIcon
	},
	{
		value: false,
		label: m.common_unused(),
		icon: AlertIcon
	}
];

export const imageUpdateFilters = [
	{
		value: true,
		label: m.images_has_updates(),
		icon: UpdateIcon
	},
	{
		value: false,
		label: m.images_no_updates(),
		icon: VerifiedCheckIcon
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
		icon: CheckIcon
	},
	{
		value: 'warning',
		label: m.events_warning(),
		icon: AlertIcon
	},
	{
		value: 'error',
		label: m.events_error(),
		icon: CloseIcon
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
