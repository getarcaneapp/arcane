import type { ApplicationTheme } from '$lib/types/settings.type';

export const APPLICATION_THEME_VALUES = [
	'default',
	'graphite',
	'ocean',
	'amber',
	'github',
	'nord',
	'everforest',
	'rosepine'
] as const satisfies readonly ApplicationTheme[];

export type ApplicationThemeOption = {
	value: ApplicationTheme;
	preview: {
		light: {
			background: string;
			sidebar: string;
			card: string;
			border: string;
			foreground: string;
			primary: string;
		};
		dark: {
			background: string;
			sidebar: string;
			card: string;
			border: string;
			foreground: string;
			primary: string;
		};
	};
};

export const APPLICATION_THEME_OPTIONS: readonly ApplicationThemeOption[] = [
	{
		value: 'default',
		preview: {
			light: {
				background: '#fafafa',
				sidebar: '#f3f4f6',
				card: '#ffffff',
				border: '#d4d4d8',
				foreground: '#18181b',
				primary: '#8b5cf6'
			},
			dark: {
				background: '#24262b',
				sidebar: '#1c1f24',
				card: '#31343a',
				border: '#4a4f58',
				foreground: '#f5f7fa',
				primary: '#a855f7'
			}
		}
	},
	{
		value: 'graphite',
		preview: {
			light: {
				background: '#eef1f5',
				sidebar: '#dfe5ec',
				card: '#f8fafc',
				border: '#cbd5e1',
				foreground: '#0f172a',
				primary: '#5b6ee1'
			},
			dark: {
				background: '#2d3139',
				sidebar: '#252932',
				card: '#343944',
				border: '#4d5664',
				foreground: '#edf2f7',
				primary: '#8ea0ff'
			}
		}
	},
	{
		value: 'ocean',
		preview: {
			light: {
				background: '#edf6fb',
				sidebar: '#d7ebf5',
				card: '#f8fcff',
				border: '#b7d7e7',
				foreground: '#0f2942',
				primary: '#0f77a8'
			},
			dark: {
				background: '#22384c',
				sidebar: '#1b2d3f',
				card: '#2a4358',
				border: '#456983',
				foreground: '#eef7fb',
				primary: '#73b9da'
			}
		}
	},
	{
		value: 'amber',
		preview: {
			light: {
				background: '#f9f1e6',
				sidebar: '#efe0cd',
				card: '#fffaf3',
				border: '#ddc2a0',
				foreground: '#3f2a12',
				primary: '#c67620'
			},
			dark: {
				background: '#3b2d1f',
				sidebar: '#2f2419',
				card: '#473626',
				border: '#7a5b3d',
				foreground: '#f7ead7',
				primary: '#e0a255'
			}
		}
	},
	{
		value: 'github',
		preview: {
			light: {
				background: '#f6f8fa',
				sidebar: '#eff2f5',
				card: '#ffffff',
				border: '#d0d7de',
				foreground: '#1f2328',
				primary: '#0969da'
			},
			dark: {
				background: '#161b22',
				sidebar: '#0d1117',
				card: '#21262d',
				border: '#30363d',
				foreground: '#e6edf3',
				primary: '#2f81f7'
			}
		}
	},
	{
		value: 'nord',
		preview: {
			light: {
				background: '#eceff4',
				sidebar: '#e5e9f0',
				card: '#f8fafc',
				border: '#c5cedc',
				foreground: '#2e3440',
				primary: '#5e81ac'
			},
			dark: {
				background: '#2b313c',
				sidebar: '#242933',
				card: '#353c49',
				border: '#4c566a',
				foreground: '#eceff4',
				primary: '#88c0d0'
			}
		}
	},
	{
		value: 'everforest',
		preview: {
			light: {
				background: '#f2efdf',
				sidebar: '#e7e2cc',
				card: '#fbf8ee',
				border: '#cdbf9f',
				foreground: '#374145',
				primary: '#6f894e'
			},
			dark: {
				background: '#2d3530',
				sidebar: '#252b28',
				card: '#343f39',
				border: '#56635f',
				foreground: '#e6e2cc',
				primary: '#a7c080'
			}
		}
	},
	{
		value: 'rosepine',
		preview: {
			light: {
				background: '#faf4ed',
				sidebar: '#f2e9e1',
				card: '#fffaf5',
				border: '#dfd1c5',
				foreground: '#575279',
				primary: '#b4637a'
			},
			dark: {
				background: '#191724',
				sidebar: '#16141f',
				card: '#26233a',
				border: '#403d52',
				foreground: '#e0def4',
				primary: '#c4a7e7'
			}
		}
	}
] as const;

const APP_THEME_ATTRIBUTE = 'data-app-theme';
const OLED_CLASS = 'oled';
const DARK_CLASS = 'dark';
const OLED_THEME_COLOR = '#000000';

let htmlClassObserver: MutationObserver | null = null;

export function resolveApplicationTheme(value?: string | null): ApplicationTheme {
	if (!value) {
		return 'default';
	}

	return APPLICATION_THEME_VALUES.includes(value as ApplicationTheme) ? (value as ApplicationTheme) : 'default';
}

export function applyApplicationTheme(themeValue?: string | null): void {
	const theme = resolveApplicationTheme(themeValue);

	if (typeof document === 'undefined') {
		return;
	}

	ensureThemeColorSyncObserver();

	if (theme === 'default') {
		document.documentElement.removeAttribute(APP_THEME_ATTRIBUTE);
	} else {
		document.documentElement.setAttribute(APP_THEME_ATTRIBUTE, theme);
	}

	syncBrowserThemeColor();
}

function isDefaultApplicationTheme(root: HTMLElement): boolean {
	const appTheme = root.getAttribute(APP_THEME_ATTRIBUTE);
	return !appTheme || appTheme === 'default';
}

function ensureThemeColorSyncObserver(): void {
	if (typeof document === 'undefined' || htmlClassObserver) {
		return;
	}

	htmlClassObserver = new MutationObserver(() => {
		syncBrowserThemeColor();
	});

	htmlClassObserver.observe(document.documentElement, {
		attributes: true,
		attributeFilter: ['class', APP_THEME_ATTRIBUTE]
	});
}

export function syncBrowserThemeColor(): void {
	if (typeof document === 'undefined') {
		return;
	}

	const root = document.documentElement;
	const isDarkMode = root.classList.contains(DARK_CLASS);
	const activeMeta = document.querySelector<HTMLMetaElement>(
		`meta[name="theme-color"][media="(prefers-color-scheme: ${isDarkMode ? 'dark' : 'light'})"]`
	);

	if (!activeMeta) {
		return;
	}

	if (isDarkMode && root.classList.contains(OLED_CLASS) && isDefaultApplicationTheme(root)) {
		activeMeta.content = OLED_THEME_COLOR;
		return;
	}

	const background = getComputedStyle(root).getPropertyValue('--background').trim();

	if (background) {
		activeMeta.content = background;
	}
}
