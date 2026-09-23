import { tv, type VariantProps } from 'tailwind-variants';

import { m } from '#lib/paraglide/messages.js';
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
	OpenIdIcon,
	RedeployIcon,
	CodeIcon,
	HammerIcon,
	PauseIcon,
	PlayIcon,
	ZapIcon,
	TagIcon,
	ScanIcon,
	ShieldCheckIcon,
	ImagesIcon,
	TestIcon,
	BoxIcon
} from '#lib/icons/index.js';

export const arcaneButtonVariants = tv({
	base:
		'inline-flex items-center justify-center gap-2 rounded-xl text-sm font-medium whitespace-nowrap select-none ' +
		'border transition duration-200 ease-out ' +
		'active:scale-98 ' +
		'disabled:pointer-events-none disabled:opacity-55 disabled:saturate-[0.82] disabled:shadow-none ' +
		'aria-disabled:pointer-events-none aria-disabled:opacity-55 aria-disabled:saturate-[0.82] aria-disabled:shadow-none ' +
		'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-2 focus-visible:ring-offset-background ' +
		"[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
	variants: {
		tone: {
			'outline-primary':
				'border-primary/20 bg-primary/[0.06] text-foreground! shadow-button ' +
				'hover:border-primary/40 hover:bg-primary/[0.1] hover:shadow-button-hover ' +
				'dark:border-primary/30 dark:bg-primary/[0.12] dark:text-primary-foreground ' +
				'dark:hover:border-primary/50 dark:hover:bg-primary/[0.18]',
			'outline-primary-login':
				'w-full border-primary/20 bg-primary/[0.06] text-foreground! shadow-button ' +
				'hover:border-primary/40 hover:bg-primary/[0.1] hover:shadow-button-hover ' +
				'dark:border-primary/30 dark:bg-primary/[0.12] dark:text-primary-foreground ' +
				'dark:hover:border-primary/50 dark:hover:bg-primary/[0.18]',
			'outline-destructive':
				'border-destructive/20 bg-destructive/[0.06] text-foreground! shadow-button ' +
				'hover:border-destructive/40 hover:bg-destructive/[0.1] hover:shadow-button-hover ' +
				'dark:border-destructive/30 dark:bg-destructive/[0.12] dark:text-destructive-foreground ' +
				'dark:hover:border-destructive/50 dark:hover:bg-destructive/[0.18]',
			'outline-success':
				'border-success/20 bg-success/[0.06] text-foreground! shadow-button ' +
				'hover:border-success/40 hover:bg-success/[0.1] hover:shadow-button-hover ' +
				'dark:border-success/30 dark:bg-success/[0.12] dark:text-success ' +
				'dark:hover:border-success/50 dark:hover:bg-success/[0.18]',
			'outline-info':
				'border-info/20 bg-info/[0.06] text-foreground! shadow-button ' +
				'hover:border-info/40 hover:bg-info/[0.1] hover:shadow-button-hover ' +
				'dark:border-info/30 dark:bg-info/[0.12] dark:text-info ' +
				'dark:hover:border-info/50 dark:hover:bg-info/[0.18]',
			'outline-warning':
				'border-warning/20 bg-warning/[0.06] text-foreground! shadow-button ' +
				'hover:border-warning/40 hover:bg-warning/[0.1] hover:shadow-button-hover ' +
				'dark:border-warning/30 dark:bg-warning/[0.12] dark:text-warning ' +
				'dark:hover:border-warning/50 dark:hover:bg-warning/[0.18]',
			'outline-tag':
				'border-teal/20 bg-teal/[0.06] text-foreground! shadow-button ' +
				'hover:border-teal/40 hover:bg-teal/[0.1] hover:shadow-button-hover ' +
				'dark:border-teal/30 dark:bg-teal/[0.12] dark:text-teal ' +
				'dark:hover:border-teal/50 dark:hover:bg-teal/[0.18]',
			'outline-scan':
				'border-indigo/20 bg-indigo/[0.06] text-foreground! shadow-button ' +
				'hover:border-indigo/40 hover:bg-indigo/[0.1] hover:shadow-button-hover ' +
				'dark:border-indigo/30 dark:bg-indigo/[0.12] dark:text-indigo ' +
				'dark:hover:border-indigo/50 dark:hover:bg-indigo/[0.18]',
			'outline-commit':
				'border-cyan/20 bg-cyan/[0.06] text-foreground! shadow-button ' +
				'hover:border-cyan/40 hover:bg-cyan/[0.1] hover:shadow-button-hover ' +
				'dark:border-cyan/30 dark:bg-cyan/[0.12] dark:text-cyan ' +
				'dark:hover:border-cyan/50 dark:hover:bg-cyan/[0.18]',
			'outline-build':
				'border-violet/20 bg-violet/[0.06] text-foreground! shadow-button ' +
				'hover:border-violet/40 hover:bg-violet/[0.1] hover:shadow-button-hover ' +
				'dark:border-violet/30 dark:bg-violet/[0.12] dark:text-violet ' +
				'dark:hover:border-violet/50 dark:hover:bg-violet/[0.18]',
			'outline-sync':
				'border-purple/20 bg-purple/[0.06] text-foreground! shadow-button ' +
				'hover:border-purple/40 hover:bg-purple/[0.1] hover:shadow-button-hover ' +
				'dark:border-purple/30 dark:bg-purple/[0.12] dark:text-purple ' +
				'dark:hover:border-purple/50 dark:hover:bg-purple/[0.18]',
			'outline-archive':
				'border-muted-foreground/20 bg-muted-foreground/[0.06] text-foreground! shadow-button ' +
				'hover:border-muted-foreground/40 hover:bg-muted-foreground/[0.1] hover:shadow-button-hover ' +
				'dark:border-muted-foreground/30 dark:bg-muted-foreground/[0.12] dark:text-muted-foreground ' +
				'dark:hover:border-muted-foreground/50 dark:hover:bg-muted-foreground/[0.18]',

			outline:
				'border-border/80 bg-background/90 text-foreground shadow-button backdrop-blur-sm ' +
				'hover:border-border hover:bg-accent/60 hover:text-accent-foreground hover:shadow-button-hover ' +
				'dark:bg-card/70 dark:hover:bg-accent/40',

			ghost:
				'border-transparent bg-transparent text-foreground! shadow-none ' +
				'hover:bg-accent/50 hover:text-accent-foreground dark:hover:bg-accent/30',
			link: 'border-transparent bg-transparent text-primary shadow-none underline-offset-4 hover:bg-primary/5 hover:underline'
		},
		size: {
			default: 'h-9 px-4 py-2 has-[svg]:px-3',
			sm: 'h-8 gap-1.5 rounded-lg px-3 has-[svg]:px-2.5',
			lg: 'h-10 rounded-xl px-5 has-[svg]:px-4',
			icon: 'size-9 p-0',
			card: 'min-h-14 w-full justify-start gap-3 rounded-2xl p-3 text-left'
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
	build: {
		defaultLabel: m.build(),
		IconComponent: HammerIcon,
		tone: 'outline-build',
		loadingLabel: m.starting_build()
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
	pause: {
		defaultLabel: m.common_pause(),
		IconComponent: PauseIcon,
		tone: 'outline-warning',
		loadingLabel: m.common_processing()
	},
	unpause: {
		defaultLabel: m.common_unpause(),
		IconComponent: PlayIcon,
		tone: 'outline-success',
		loadingLabel: m.common_processing()
	},
	kill: {
		defaultLabel: m.common_kill(),
		IconComponent: ZapIcon,
		tone: 'outline-destructive',
		loadingLabel: m.common_action_killing()
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
		defaultLabel: m.pull(),
		IconComponent: DownloadIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_pulling()
	},
	tag: {
		defaultLabel: m.images_tag_image(),
		IconComponent: TagIcon,
		tone: 'outline-tag',
		loadingLabel: m.common_processing()
	},
	scan: {
		defaultLabel: m.vuln_scan(),
		IconComponent: ScanIcon,
		tone: 'outline-scan',
		loadingLabel: m.common_processing()
	},
	patch: {
		defaultLabel: m.images_patch(),
		IconComponent: ShieldCheckIcon,
		tone: 'outline-scan',
		loadingLabel: m.common_processing()
	},
	test: {
		defaultLabel: m.test_connection(),
		IconComponent: TestIcon,
		tone: 'outline-info',
		loadingLabel: m.environments_testing_connection()
	},
	archive: {
		defaultLabel: m.projects_archive(),
		IconComponent: BoxIcon,
		tone: 'outline-archive',
		loadingLabel: m.common_processing()
	},
	commit: {
		defaultLabel: m.commit(),
		IconComponent: ImagesIcon,
		tone: 'outline-commit',
		loadingLabel: m.common_processing()
	},
	redeploy: {
		defaultLabel: m.common_redeploy(),
		IconComponent: RedeployIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_action_redeploying()
	},
	refresh: {
		defaultLabel: m.common_refresh(),
		IconComponent: RefreshIcon,
		tone: 'outline-primary',
		loadingLabel: m.common_refresh()
	},
	sync: {
		defaultLabel: m.resource_sync_cap(),
		IconComponent: RefreshIcon,
		tone: 'outline-sync',
		loadingLabel: m.common_syncing()
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
	json: {
		defaultLabel: m.common_parsed(),
		IconComponent: CodeIcon,
		tone: 'outline',
		loadingLabel: m.common_processing()
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
