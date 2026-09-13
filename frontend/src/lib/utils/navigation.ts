import { browser } from '$app/env';
import { PersistedState } from 'runed';
import { createContext } from 'svelte';
import {
	DEFAULT_LANDING_PAGE,
	defaultMobileNavigationSettings,
	getLandingPageNavItems,
	type MobileNavigationSettings
} from '#lib/config/navigation-config.js';
import userStore from '#lib/stores/user-store.svelte.js';

// --- Mobile nav state ---

const pinnedItemsStore = new PersistedState('mobile-nav-settings', defaultMobileNavigationSettings);

export const [getMobileNavigation, setMobileNavigation] = createContext<{
	readonly settings: MobileNavigationSettings;
	visible: boolean;
}>();

export function getEffectiveNavigationSettings(): MobileNavigationSettings {
	const preferences = userStore.current?.preferences;
	const mode = preferences?.mobileNavigationMode ?? defaultMobileNavigationSettings.mode;

	return {
		pinnedItems: pinnedItemsStore.current.pinnedItems,
		mode,
		showLabels: preferences?.mobileNavigationShowLabels ?? defaultMobileNavigationSettings.showLabels,
		scrollToHide: mode === 'floating'
	};
}

/**
 * Resolve the page to open after signing in from the signed-in user's account
 * preference, falling back to the built-in default. A stale value (e.g. a page
 * that no longer exists) falls back to the default rather than 404ing.
 */
export function getEffectiveLandingPage(): string {
	const candidate = userStore.current?.preferences?.defaultLandingPage ?? DEFAULT_LANDING_PAGE;

	return getLandingPageNavItems().some((item) => item.url === candidate) ? candidate : DEFAULT_LANDING_PAGE;
}

// --- Keyboard shortcuts ---

export type ShortcutKey = 'mod' | 'shift' | 'alt' | 'ctrl' | 'meta' | string;

const MODIFIER_KEYS = new Set(['mod', 'shift', 'alt', 'ctrl', 'meta']);

function isMacOS(): boolean {
	if (!browser) return false;
	const platform = navigator?.platform?.toLowerCase() ?? '';
	const userAgent = navigator?.userAgent?.toLowerCase() ?? '';
	return platform.includes('mac') || userAgent.includes('mac');
}

export function formatShortcutKeys(keys: ShortcutKey[], isMac = isMacOS()): string[] {
	return keys.map((key) => formatShortcutKey(key, isMac));
}

export function matchesShortcutEvent(keys: ShortcutKey[], event: KeyboardEvent, isMac = isMacOS()): boolean {
	const normalizedKeys = keys.map((key) => key.toLowerCase());
	const requiredModifiers = {
		shift: normalizedKeys.includes('shift'),
		alt: normalizedKeys.includes('alt'),
		ctrl: normalizedKeys.includes('ctrl'),
		meta: normalizedKeys.includes('meta'),
		mod: normalizedKeys.includes('mod')
	};

	const requiredCtrl = requiredModifiers.ctrl || (!isMac && requiredModifiers.mod);
	const requiredMeta = requiredModifiers.meta || (isMac && requiredModifiers.mod);
	const requiredShift = requiredModifiers.shift;
	const requiredAlt = requiredModifiers.alt;

	if (event.shiftKey !== requiredShift) return false;
	if (event.altKey !== requiredAlt) return false;
	if (event.ctrlKey !== requiredCtrl) return false;
	if (event.metaKey !== requiredMeta) return false;

	const nonModifierKeys = normalizedKeys.filter((key) => !MODIFIER_KEYS.has(key));
	if (nonModifierKeys.length !== 1) return false;

	const key = event.key.toLowerCase();
	if (MODIFIER_KEYS.has(key)) return false;

	const primaryKey = nonModifierKeys[0];
	if (!primaryKey) return false;

	const expectedCode = getExpectedCode(primaryKey);
	if (expectedCode) {
		return event.code.toLowerCase() === expectedCode;
	}

	return key === primaryKey;
}

export function isEditableTarget(target: EventTarget | null): boolean {
	if (!(target instanceof HTMLElement)) return false;
	const tagName = target.tagName.toLowerCase();
	if (['input', 'textarea', 'select'].includes(tagName)) return true;
	if (target.isContentEditable) return true;
	return !!target.closest('[contenteditable="true"]');
}

const shortcutLabels = new Map<string, readonly [string, string]>([
	['mod', ['Ctrl', '⌘']],
	['shift', ['Shift', '⇧']],
	['alt', ['Alt', '⌥']],
	['ctrl', ['Ctrl', '⌃']],
	['meta', ['Win', '⌘']]
]);

function formatShortcutKey(key: ShortcutKey, isMac: boolean): string {
	const labels = shortcutLabels.get(key);
	if (labels) return labels[isMac ? 1 : 0];
	return key.length === 1 ? key.toUpperCase() : key;
}

function getExpectedCode(key: string): string | null {
	if (/^[0-9]$/.test(key)) {
		return `digit${key}`;
	}
	if (/^[a-z]$/.test(key)) {
		return `key${key}`;
	}
	return null;
}

// --- URL helpers ---

export function toPortHref(hostPort: string, baseServerUrl?: string): string {
	try {
		const base = baseServerUrl || (typeof window !== 'undefined' ? window.location.origin : 'http://localhost');
		const scheme = hostPort.endsWith('443') ? 'https' : 'http';
		const host = afterSubstring(base, '://');

		const url = new URL(`${scheme}://${host}`);
		url.port = hostPort;
		return url.toString();
	} catch {
		return '#';
	}
}

function afterSubstring(text: string, search: string): string {
	const index = text.indexOf(search);
	return index === -1 ? text : text.slice(index + search.length);
}

export function toSafeHref(raw: string, scheme: string = 'https'): string {
	const trimmed = raw.trim();
	if (!trimmed) return '#';
	if (/^(javascript|data|vbscript):/i.test(trimmed)) return '#';
	if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(trimmed)) return trimmed;
	return `${scheme}://${trimmed}`;
}

// --- Git URL helpers ---

type GitRoute = 'commit' | 'tree' | 'blob' | 'edit';

function stripGitSuffix(path: string): string {
	return path.replace(/\.git\/?$/, '').replace(/\/+$/, '');
}

function encodePathSegments(value: string): string {
	return value
		.split('/')
		.filter(Boolean)
		.map((segment) => encodeURIComponent(segment))
		.join('/');
}

export function toGitWebUrl(raw: string): string | null {
	const trimmed = raw.trim();
	if (!trimmed) return null;

	if (trimmed.includes('://')) {
		try {
			const parsed = new URL(trimmed);
			if (!parsed.hostname) return null;
			const path = stripGitSuffix(parsed.pathname);
			if (!path || path === '/') return null;
			const isWebProtocol = parsed.protocol === 'http:' || parsed.protocol === 'https:';
			const protocol = isWebProtocol ? parsed.protocol : 'https:';
			// An ssh:// port is the SSH port, not the forge's web port.
			const host = isWebProtocol ? parsed.host : parsed.hostname;
			return `${protocol}//${host}${path}`;
		} catch {
			return null;
		}
	}

	const match = /^(?:.+@)?([^:\/]+):(.+)$/.exec(trimmed) ?? /^([^\/]+)\/(.+)$/.exec(trimmed);
	if (!match) return null;
	const host = match[1];
	const matchedPath = match[2];
	if (!host || !matchedPath) return null;
	const path = stripGitSuffix(matchedPath.replace(/^\/+/, ''));
	return path ? `https://${host}/${path}` : null;
}

// Only hostnames that name their product are recognized; a self-hosted forge on a neutral domain gets no deep link.
function gitRouteSegment(hostname: string, route: GitRoute): string | null {
	const host = hostname.toLowerCase();
	if (host.includes('gitlab')) return `/-/${route}/`;
	// Bitbucket has no dedicated edit route; /src/ opens the file with its own edit action.
	if (host.includes('bitbucket')) return route === 'commit' ? '/commits/' : '/src/';
	if (host.includes('gitea') || host.includes('forgejo') || host.includes('codeberg')) {
		if (route === 'edit') return '/_edit/';
		return route === 'commit' ? '/commit/' : '/src/branch/';
	}
	if (host.includes('github') || route === 'commit') return `/${route}/`;
	return null;
}

export function toGitRouteUrl(repositoryUrl: string, route: GitRoute, ref: string, path = ''): string | null {
	const base = toGitWebUrl(repositoryUrl);
	const encodedRef = encodePathSegments(ref.trim());
	if (!base || !encodedRef) return null;

	try {
		const segment = gitRouteSegment(new URL(base).hostname, route);
		if (!segment) return null;
		const encodedPath = encodePathSegments(path);
		return `${base}${segment}${encodedRef}${encodedPath ? `/${encodedPath}` : ''}`;
	} catch {
		return null;
	}
}
