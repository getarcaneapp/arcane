import { browser } from '$app/env';
import { PersistedState } from 'runed';
import { get } from 'svelte/store';
import {
	DEFAULT_LANDING_PAGE,
	defaultMobileNavigationSettings,
	getLandingPageNavItems,
	type MobileNavigationSettings
} from '#lib/config/navigation-config';
import userStore from '#lib/stores/user-store';

// --- Mobile nav state ---

const pinnedItemsStore = new PersistedState('mobile-nav-settings', defaultMobileNavigationSettings);

type NavigationVisibilityController = {
	resetVisibility: () => void;
};

let mobileNavController: NavigationVisibilityController | null = null;

export function registerNavigationVisibilityController(controller: NavigationVisibilityController | null) {
	mobileNavController = controller;
}

export function resetNavigationVisibility() {
	mobileNavController?.resetVisibility();
}

export function getEffectiveNavigationSettings(): MobileNavigationSettings {
	const preferences = get(userStore)?.preferences;
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
	const candidate = get(userStore)?.preferences?.defaultLandingPage ?? DEFAULT_LANDING_PAGE;

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

function formatShortcutKey(key: ShortcutKey, isMac: boolean): string {
	switch (key) {
		case 'mod':
			return isMac ? '⌘' : 'Ctrl';
		case 'shift':
			return isMac ? '⇧' : 'Shift';
		case 'alt':
			return isMac ? '⌥' : 'Alt';
		case 'ctrl':
			return isMac ? '⌃' : 'Ctrl';
		case 'meta':
			return isMac ? '⌘' : 'Win';
		default:
			return key.length === 1 ? key.toUpperCase() : key;
	}
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

function stripGitSuffix(path: string): string {
	return path.replace(/\.git\/?$/, '');
}

function trimTrailingSlash(value: string): string {
	return value.replace(/\/+$/, '');
}

function commitSegmentForHost(hostname: string): string {
	const host = hostname.toLowerCase();
	if (host.includes('gitlab')) return '/-/commit/';
	if (host.includes('bitbucket')) return '/commits/';
	return '/commit/';
}

function toGitWebUrl(raw: string): string | null {
	const trimmed = raw.trim();
	if (!trimmed) return null;

	if (trimmed.includes('://')) {
		try {
			const parsed = new URL(trimmed);
			if (!parsed.hostname) return null;
			const path = stripGitSuffix(parsed.pathname);
			if (!path || path === '/') return null;
			const protocol = parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.protocol : 'https:';
			return `${protocol}//${parsed.hostname}${path}`;
		} catch {
			return null;
		}
	}

	const scpMatch = /^(?:.+@)?([^:\/]+):(.+)$/.exec(trimmed);
	if (scpMatch) {
		const host = scpMatch[1];
		const matchedPath = scpMatch[2];
		if (!host || !matchedPath) return null;

		const path = stripGitSuffix(matchedPath.replace(/^\/+/, ''));
		if (!host || !path) return null;
		return `https://${host}/${path}`;
	}

	const hostPathMatch = /^([^\/]+)\/(.+)$/.exec(trimmed);
	if (hostPathMatch) {
		const host = hostPathMatch[1];
		const matchedPath = hostPathMatch[2];
		if (!host || !matchedPath) return null;

		const path = stripGitSuffix(matchedPath.replace(/^\/+/, ''));
		if (!host || !path) return null;
		return `https://${host}/${path}`;
	}

	return null;
}

export function toGitCommitUrl(repositoryUrl: string, commit: string): string | null {
	const base = toGitWebUrl(repositoryUrl);
	const trimmedCommit = commit.trim();
	if (!base || !trimmedCommit) return null;

	const normalizedBase = trimTrailingSlash(base);
	try {
		const host = new URL(normalizedBase).hostname;
		const segment = commitSegmentForHost(host);
		return `${normalizedBase}${segment}${encodeURIComponent(trimmedCommit)}`;
	} catch {
		return null;
	}
}
