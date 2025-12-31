import { tv, type VariantProps } from 'tailwind-variants';

import { m } from '$lib/paraglide/messages';
import {
	StopIcon,
	StartIcon,
	RefreshIcon,
	DownloadIcon,
	TrashIcon,
	UpdateIcon,
	EditIcon,
	CheckIcon,
	AddIcon,
	CloseIcon,
	SaveIcon,
	RestartIcon,
	InspectIcon,
	FileTextIcon,
	TemplateIcon,
	type IconType,
	LoginIcon,
	OpenIdIcon
} from '$lib/icons';

export const arcaneButtonVariants = tv({
	base:
		'inline-flex items-center justify-center gap-2 rounded-lg text-sm font-medium whitespace-nowrap select-none ' +
		'transition-all duration-200 ' +
		'active:scale-[0.98] ' +
		'border disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50 ' +
		'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-offset-0 ' +
		"[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
	variants: {
		tone: {
			'outline-primary':
				'bg-primary/5 text-foreground! border-primary/20 hover:bg-primary/10 hover:border-primary/40 ' +
				'dark:bg-primary/10 dark:text-primary-foreground dark:border-primary/30 dark:hover:bg-primary/20 ' +
				'shadow-sm hover:shadow-md',
			'outline-primary-login':
				'bg-primary/5 text-foreground! border-primary/20 hover:bg-primary/10 hover:border-primary/40 ' +
				'dark:bg-primary/10 dark:text-primary-foreground dark:border-primary/30 dark:hover:bg-primary/20 ' +
				'shadow-sm hover:shadow-md w-full',
			'outline-destructive':
				'bg-destructive/5 text-foreground! border-destructive/20 hover:bg-destructive/10 hover:border-destructive/40 ' +
				'dark:bg-destructive/10 dark:text-destructive-foreground dark:border-destructive/30 dark:hover:bg-destructive/20 ' +
				'shadow-sm hover:shadow-md',
			'outline-success':
				'bg-emerald-500/5 text-foreground! border-emerald-500/20 hover:bg-emerald-500/10 hover:border-emerald-500/40 ' +
				'dark:bg-emerald-500/10 dark:text-emerald-400 dark:border-emerald-500/30 dark:hover:bg-emerald-500/20 ' +
				'shadow-sm hover:shadow-md',
			'outline-info':
				'bg-sky-500/5 text-foreground! border-sky-500/20 hover:bg-sky-500/10 hover:border-sky-500/40 ' +
				'dark:bg-sky-500/10 dark:text-sky-400 dark:border-sky-500/30 dark:hover:bg-sky-500/20 ' +
				'shadow-sm hover:shadow-md',

			outline: 'bg-background border-input hover:bg-accent hover:text-accent-foreground shadow-sm',

			ghost:
				'border-transparent bg-transparent text-foreground! hover:bg-accent/40 hover:text-accent-foreground shadow-none hover:shadow-none',
			link: 'border-transparent bg-transparent text-primary underline-offset-4 hover:underline shadow-none hover:shadow-none'
		},
		size: {
			default: 'h-9 px-4 py-2 has-[svg]:px-3',
			sm: 'h-8 gap-1.5 rounded-md px-3 has-[svg]:px-2.5',
			lg: 'h-10 rounded-md px-5 has-[svg]:px-4',
			icon: 'size-9',
			card: 'min-h-14 w-full p-3 rounded-2xl justify-start gap-3'
		},
		hoverEffect: {
			none: '',
			lift: 'hover-lift'
		}
	},
	defaultVariants: {
		tone: 'outline-primary',
		size: 'default',
		hoverEffect: 'none'
	}
});

export type ArcaneButtonTone = VariantProps<typeof arcaneButtonVariants>['tone'];
export type ArcaneButtonSize = VariantProps<typeof arcaneButtonVariants>['size'];
export type ArcaneButtonHoverEffect = VariantProps<typeof arcaneButtonVariants>['hoverEffect'];

export type ActionConfig = {
	defaultLabel?: string;
	IconComponent?: IconType;
	tone: ArcaneButtonTone;
	loadingLabel?: string;
};

export const actionConfigs = {
	base: {
		tone: 'outline'
	},
	start: {
		defaultLabel: m.common_start(),
		IconComponent: StartIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_starting()
	},
	start_all: {
		defaultLabel: m.quick_actions_start_all(),
		IconComponent: StartIcon,
		tone: 'outline-success',
		loadingLabel: m.common_action_starting()
	},
	deploy: {
		defaultLabel: m.common_up(),
		IconComponent: StartIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_deploying()
	},
	stop: {
		defaultLabel: m.common_stop(),
		IconComponent: StopIcon,
		tone: 'outline-destructive',
		loadingLabel: m.common_action_stopping()
	},
	stop_all: {
		defaultLabel: m.quick_actions_stop_all(),
		IconComponent: StopIcon,
		tone: 'outline-info',
		loadingLabel: m.common_action_stopping()
	},
	remove: {
		defaultLabel: m.common_remove(),
		IconComponent: TrashIcon,
		tone: 'outline-destructive',
		loadingLabel: m.common_action_removing()
	},
	prune: {
		defaultLabel: m.quick_actions_prune_system(),
		IconComponent: TrashIcon,
		tone: 'outline-destructive',
		loadingLabel: m.common_action_removing()
	},
	restart: {
		defaultLabel: m.common_restart(),
		IconComponent: RestartIcon,
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
		IconComponent: RestartIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_redeploying()
	},
	refresh: {
		defaultLabel: m.common_refresh(),
		IconComponent: RefreshIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_refresh()
	},
	inspect: {
		defaultLabel: m.common_inspect(),
		IconComponent: InspectIcon,
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
		IconComponent: AddIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_creating()
	},
	template: {
		defaultLabel: m.common_use_template(),
		IconComponent: TemplateIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_creating()
	},
	logs: {
		defaultLabel: m.common_logs(),
		IconComponent: FileTextIcon,
		tone: 'ghost',
		loadingLabel: m.common_action_fetching_logs()
	},
	cancel: {
		defaultLabel: m.common_cancel(),
		IconComponent: CloseIcon,
		tone: 'ghost',
		loadingLabel: m.common_action_cancelling()
	},
	update: {
		defaultLabel: m.common_update(),
		IconComponent: UpdateIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_updating()
	},
	login: {
		defaultLabel: m.auth_signin_button(),
		IconComponent: LoginIcon,
		tone: 'outline-primary-login',
		loadingLabel: m.auth_signing_in()
	},
	oidc_login: {
		defaultLabel: m.auth_oidc_signin(),
		IconComponent: OpenIdIcon,
		tone: 'outline-primary-login',
		loadingLabel: m.auth_signing_in()
	}
} satisfies Record<string, ActionConfig>;

export type Action = keyof typeof actionConfigs;
