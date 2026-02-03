import { browser } from '$app/environment';

export type ShortcutKey = 'mod' | 'shift' | 'alt' | 'ctrl' | 'meta' | string;

const MODIFIER_KEYS = new Set(['mod', 'shift', 'alt', 'ctrl', 'meta']);

export function isMacOS(): boolean {
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

	const expectedCode = getExpectedCode(nonModifierKeys[0]);
	if (expectedCode) {
		return event.code.toLowerCase() === expectedCode;
	}

	return key === nonModifierKeys[0];
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
