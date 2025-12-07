import { tv, type VariantProps } from 'tailwind-variants';

import CircleStopIcon from 'phosphor-svelte/lib/StopCircle';
import PlayIcon from 'phosphor-svelte/lib/Play';
import RotateCcwIcon from 'phosphor-svelte/lib/ArrowCounterClockwise';
import DownloadIcon from 'phosphor-svelte/lib/Download';
import Trash2Icon from 'phosphor-svelte/lib/Trash';
import RefreshCcwDotIcon from 'phosphor-svelte/lib/ArrowsClockwise';
import RefreshCcwIcon from 'phosphor-svelte/lib/ArrowsClockwise';
import ScanSearchIcon from 'phosphor-svelte/lib/MagnifyingGlass';
import FileTextIcon from 'phosphor-svelte/lib/FileText';
import EditIcon from 'phosphor-svelte/lib/PencilSimple';
import CheckIcon from 'phosphor-svelte/lib/Check';
import XIcon from 'phosphor-svelte/lib/X';
import SaveIcon from 'phosphor-svelte/lib/FloppyDisk';
import PlusCircleIcon from 'phosphor-svelte/lib/PlusCircle';
import LayoutTemplateIcon from 'phosphor-svelte/lib/Layout';
import { m } from '$lib/paraglide/messages';

export const arcaneButtonVariants = tv({
	base:
		'inline-flex items-center justify-center gap-2 rounded-lg text-sm font-medium whitespace-nowrap select-none ' +
		'transition-[transform,box-shadow,background-color,border-color,color] duration-150 will-change-transform hover:-translate-y-0.5 active:translate-y-0 ' +
		'border disabled:pointer-events-none disabled:opacity-50 ' +
		'focus-visible:outline-none focus-visible:ring-0 ring-0 ring-offset-0 ' +
		"[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
	variants: {
		tone: {
			'outline-primary':
				'bg-transparent text-muted-foreground border-primary/70 hover:bg-primary/15 hover:border-primary/75 ' +
				'dark:text-muted-foreground dark:border-primary/60 dark:hover:bg-primary/20 shadow-none hover:shadow-none',
			'outline-destructive':
				'bg-transparent text-destructive/75 border-destructive/60 hover:bg-destructive/10 hover:border-destructive/65 ' +
				'dark:text-destructive/75 dark:border-destructive/55 dark:hover:bg-destructive/14 shadow-none hover:shadow-none',

			ghost: 'border-transparent bg-transparent text-foreground hover:bg-accent/40 shadow-none hover:shadow-none',
			link: 'border-transparent bg-transparent text-primary underline-offset-4 hover:underline shadow-none hover:shadow-none'
		},
		size: {
			default: 'h-9 px-4 py-2 has-[>svg]:px-3',
			sm: 'h-8 gap-1.5 rounded-md px-3 has-[>svg]:px-2.5',
			lg: 'h-10 rounded-md px-5 has-[>svg]:px-4',
			icon: 'size-9'
		}
	},
	defaultVariants: {
		tone: 'outline-primary',
		size: 'default'
	}
});

export type ArcaneButtonTone = VariantProps<typeof arcaneButtonVariants>['tone'];
export type ArcaneButtonSize = VariantProps<typeof arcaneButtonVariants>['size'];

export type Action =
	| 'start'
	| 'deploy'
	| 'stop'
	| 'restart'
	| 'remove'
	| 'pull'
	| 'redeploy'
	| 'refresh'
	| 'inspect'
	| 'logs'
	| 'edit'
	| 'confirm'
	| 'cancel'
	| 'save'
	| 'create'
	| 'template';

export type IconCtor = typeof PlayIcon;

export type ActionConfig = {
	defaultLabel: string;
	IconComponent: IconCtor;
	tone: ArcaneButtonTone;
	loadingLabel?: string;
};

export const actionConfigs: Record<Action, ActionConfig> = {
	start: {
		defaultLabel: m.common_start(),
		IconComponent: PlayIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_starting()
	},
	deploy: {
		defaultLabel: m.common_up(),
		IconComponent: PlayIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_deploying()
	},
	stop: {
		defaultLabel: m.common_stop(),
		IconComponent: CircleStopIcon,
		tone: 'outline-destructive',
		loadingLabel: m.common_action_stopping()
	},
	remove: {
		defaultLabel: m.common_remove(),
		IconComponent: Trash2Icon,
		tone: 'outline-destructive',
		loadingLabel: m.common_action_removing()
	},
	restart: {
		defaultLabel: m.common_restart(),
		IconComponent: RotateCcwIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_restarting()
	},
	pull: {
		defaultLabel: m.images_pull(),
		IconComponent: DownloadIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_pulling()
	},
	redeploy: {
		defaultLabel: m.common_redeploy(),
		IconComponent: RefreshCcwDotIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_redeploying()
	},
	refresh: {
		defaultLabel: m.common_refresh(),
		IconComponent: RefreshCcwIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_refresh()
	},
	inspect: {
		defaultLabel: m.common_inspect(),
		IconComponent: ScanSearchIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_inspecting()
	},
	edit: { defaultLabel: m.common_edit(), IconComponent: EditIcon, tone: 'outline-primary', loadingLabel: m.common_saving() },
	confirm: {
		defaultLabel: m.common_confirm(),
		IconComponent: CheckIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_confirming()
	},
	save: { defaultLabel: m.common_save(), IconComponent: SaveIcon, tone: 'outline-primary', loadingLabel: m.common_saving() },
	create: {
		defaultLabel: m.common_create(),
		IconComponent: PlusCircleIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_creating()
	},
	template: {
		defaultLabel: m.common_use_template(),
		IconComponent: LayoutTemplateIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_creating()
	},
	logs: {
		defaultLabel: m.common_logs(),
		IconComponent: FileTextIcon,
		tone: 'ghost',
		loadingLabel: m.common_action_fetching_logs()
	},
	cancel: { defaultLabel: m.common_cancel(), IconComponent: XIcon, tone: 'ghost', loadingLabel: m.common_action_cancelling() }
};
