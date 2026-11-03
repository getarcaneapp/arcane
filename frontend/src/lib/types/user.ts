import type { Locale } from '#lib/paraglide/runtime.js';

import type { DashboardWidgetType } from './ui/user';

export type TimeFormat = 'auto' | '12h' | '24h';

export type ApplicationTheme = string;

export type IconCatalog = 'selfhst' | 'dashboard-icons';

export type ProjectEditorLayout = 'auto' | 'classic' | 'tree';

export type EnvironmentLabelStyle = 'name' | 'dot';

export type EditorTabWidth = 2 | 4 | 8;

export type ToastPosition = 'auto' | 'top-left' | 'top-right' | 'top-center' | 'bottom-left' | 'bottom-right' | 'bottom-center';

export type DashboardResourceKind = 'containers' | 'projects' | 'images' | 'volumes' | 'networks';

export type ThemePalette = {
	background: string;
	foreground: string;
	card: string;
	'card-foreground': string;
	popover: string;
	'popover-foreground': string;
	primary: string;
	'primary-foreground': string;
	secondary: string;
	'secondary-foreground': string;
	muted: string;
	'muted-foreground': string;
	accent: string;
	'accent-foreground': string;
	border: string;
	input: string;
	ring: string;
	sidebar: string;
	'sidebar-foreground': string;
	'sidebar-primary': string;
	'sidebar-primary-foreground': string;
	'sidebar-accent': string;
	'sidebar-accent-foreground': string;
	'sidebar-border': string;
	'sidebar-ring': string;
	surface: string;
	'glass-shadow-color': string;
	'syntax-comment': string;
	'syntax-keyword': string;
	'syntax-property': string;
	'syntax-string': string;
	'syntax-number': string;
	'syntax-operator': string;
};

export type CustomTheme = {
	id: string;
	name: string;
	description?: string;
	light: ThemePalette;
	dark: ThemePalette;
};

export type DashboardWidgetConfig = {
	environmentIds?: string[];
	resourceKind?: DashboardResourceKind;
	resourceId?: string;
	search?: string;
	filters?: Record<string, string>;
	display?: 'count' | 'list';
	limit?: number;
};

export type DashboardWidgetLayout = {
	id?: string;
	type?: DashboardWidgetType;
	title?: string;
	x: number;
	y: number;
	w: number;
	h: number;
	hidden?: boolean;
	config?: DashboardWidgetConfig;
};

export type UserPreferences = {
	themeMode?: 'light' | 'dark' | 'system';
	applicationTheme?: ApplicationTheme;
	customThemes?: CustomTheme[];
	accentColor?: string;
	iconCatalog?: IconCatalog;
	oledMode?: boolean;
	glassEffectsEnabled?: boolean;
	animationsEnabled?: boolean;
	sidebarHoverExpansion?: boolean;
	keyboardShortcutsEnabled?: boolean;
	mobileNavigationMode?: 'floating' | 'docked';
	mobileNavigationShowLabels?: boolean;
	defaultLandingPage?: string;
	defaultProjectEditorLayout?: ProjectEditorLayout;
	editorTabWidth?: EditorTabWidth;
	toastPosition?: ToastPosition;
	environmentColors?: Record<string, string>;
	environmentLabelStyle?: EnvironmentLabelStyle;
	dashboardLayout?: DashboardWidgetLayout[];
};

export type RoleAssignmentSummary = {
	roleId: string;
	environmentId?: string;
	source: 'manual' | 'oidc';
};

export type User = {
	id: string;
	username: string;
	password?: string;
	displayName?: string | null;
	email?: string | null;
	avatarUrl?: string;
	roleAssignments: RoleAssignmentSummary[];
	permissionsByEnv: Record<string, string[]>;
	isGlobalAdmin: boolean;
	canDelete: boolean;
	createdAt?: string;
	lastLogin?: string;
	updatedAt?: string;
	oidcSubjectId?: string;
	locale?: Locale | null;
	timeFormat: TimeFormat;
	fontSize?: number;
	preferences: UserPreferences;
	requiresPasswordChange: boolean;
};
