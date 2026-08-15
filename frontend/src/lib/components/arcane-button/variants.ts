import { tv, type VariantProps } from 'tailwind-variants';

import { m } from '#lib/paraglide/messages';
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
	ImagesIcon,
	TestIcon,
	BoxIcon
} from '#lib/icons';

export const arcaneButtonVariants = tv({
	base:
		'inline-flex items-center justify-center gap-2 rounded-xl text-sm font-medium whitespace-nowrap select-none ' +
		'border transition-[background-color,border-color,color,box-shadow,transform,filter] duration-200 ease-out ' +
		'active:scale-[0.985] ' +
		'disabled:pointer-events-none disabled:opacity-55 disabled:saturate-[0.82] disabled:shadow-none ' +
		'aria-disabled:pointer-events-none aria-disabled:opacity-55 aria-disabled:saturate-[0.82] aria-disabled:shadow-none ' +
		'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-2 focus-visible:ring-offset-background ' +
		"[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
	variants: {
		tone: {
			'outline-primary':
				'border-primary/20 bg-primary/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-primary/40 hover:bg-primary/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-primary/30 dark:bg-primary/[0.12] dark:text-primary-foreground dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-primary/50 dark:hover:bg-primary/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-primary-login':
				'w-full border-primary/20 bg-primary/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-primary/40 hover:bg-primary/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-primary/30 dark:bg-primary/[0.12] dark:text-primary-foreground dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-primary/50 dark:hover:bg-primary/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-destructive':
				'border-destructive/20 bg-destructive/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-destructive/40 hover:bg-destructive/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-destructive/30 dark:bg-destructive/[0.12] dark:text-destructive-foreground dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-destructive/50 dark:hover:bg-destructive/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-success':
				'border-emerald-500/20 bg-emerald-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-emerald-500/40 hover:bg-emerald-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-emerald-500/30 dark:bg-emerald-500/[0.12] dark:text-emerald-300 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-emerald-500/50 dark:hover:bg-emerald-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-info':
				'border-sky-500/20 bg-sky-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-sky-500/40 hover:bg-sky-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-sky-500/30 dark:bg-sky-500/[0.12] dark:text-sky-300 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-sky-500/50 dark:hover:bg-sky-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-warning':
				'border-amber-500/20 bg-amber-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-amber-500/40 hover:bg-amber-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-amber-500/30 dark:bg-amber-500/[0.12] dark:text-amber-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-amber-500/50 dark:hover:bg-amber-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-tag':
				'border-teal-500/20 bg-teal-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-teal-500/40 hover:bg-teal-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-teal-500/30 dark:bg-teal-500/[0.12] dark:text-teal-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-teal-500/50 dark:hover:bg-teal-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-scan':
				'border-indigo-500/20 bg-indigo-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-indigo-500/40 hover:bg-indigo-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-indigo-500/30 dark:bg-indigo-500/[0.12] dark:text-indigo-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-indigo-500/50 dark:hover:bg-indigo-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-commit':
				'border-cyan-500/20 bg-cyan-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-cyan-500/40 hover:bg-cyan-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-cyan-500/30 dark:bg-cyan-500/[0.12] dark:text-cyan-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-cyan-500/50 dark:hover:bg-cyan-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-build':
				'border-violet-500/20 bg-violet-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-violet-500/40 hover:bg-violet-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-violet-500/30 dark:bg-violet-500/[0.12] dark:text-violet-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-violet-500/50 dark:hover:bg-violet-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-sync':
				'border-purple-500/20 bg-purple-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-purple-500/40 hover:bg-purple-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-purple-500/30 dark:bg-purple-500/[0.12] dark:text-purple-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-purple-500/50 dark:hover:bg-purple-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',
			'outline-archive':
				'border-slate-500/20 bg-slate-500/[0.06] text-foreground! shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(15,23,42,0.06)] ' +
				'hover:border-slate-500/40 hover:bg-slate-500/[0.1] hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.28)] ' +
				'dark:border-slate-500/30 dark:bg-slate-500/[0.12] dark:text-slate-200 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_1px_2px_rgba(0,0,0,0.18)] ' +
				'dark:hover:border-slate-500/50 dark:hover:bg-slate-500/[0.18] dark:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_10px_24px_-14px_rgba(0,0,0,0.5)]',

			outline:
				'border-border/80 bg-background/90 text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.34),0_1px_2px_rgba(15,23,42,0.05)] backdrop-blur-sm ' +
				'hover:border-border hover:bg-accent/60 hover:text-accent-foreground hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.22)] ' +
				'dark:bg-card/70 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.06),0_1px_2px_rgba(0,0,0,0.16)] dark:hover:bg-accent/40',

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
