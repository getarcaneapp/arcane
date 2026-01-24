import { m } from '$lib/paraglide/messages';
import {
	GlobeIcon,
	FolderOpenIcon,
	VerifiedCheckIcon,
	AlertIcon,
	InfoIcon,
	CloseIcon,
	CheckIcon,
	UpdateIcon,
	StartIcon,
	StopIcon
} from '$lib/icons';

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
		value: 'has_update',
		label: m.images_has_updates(),
		icon: UpdateIcon
	},
	{
		value: 'up_to_date',
		label: m.images_no_updates(),
		icon: VerifiedCheckIcon
	},
	{
		value: 'error',
		label: m.common_error(),
		icon: CloseIcon
	},
	{
		value: 'unknown',
		label: m.common_unknown(),
		icon: InfoIcon
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

export const projectStatusFilters = [
	{
		value: 'running',
		label: m.common_running(),
		icon: StartIcon
	},
	{
		value: 'stopped',
		label: m.common_stopped(),
		icon: StopIcon
	},
	{
		value: 'partially running',
		label: m.projects_status_partial(),
		icon: AlertIcon
	},
	{
		value: 'unknown',
		label: m.common_unknown(),
		icon: InfoIcon
	}
];
