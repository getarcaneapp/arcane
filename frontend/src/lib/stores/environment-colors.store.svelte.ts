import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';

// Mirrors the iOS app: one unique color per environment ID. Picks are saved in user preferences;
// environments without one get the next free palette color, in ID order.
export const environmentColorPalette = [
	'#2680C2',
	'#D06820',
	'#289B66',
	'#9657CA',
	'#C64574',
	'#168D97',
	'#AB841A',
	'#646DD6',
	'#BB5142',
	'#678B36',
	'#A453A5',
	'#477D91'
];

function normalizeColor(color: string): string {
	return color.startsWith('#') ? color.toUpperCase() : color.toLowerCase();
}

function availableColor(used: Set<string>): string {
	const color = environmentColorPalette.find((candidate) => !used.has(candidate));
	if (color) return color;
	// Walk the RGB space without repeating colors once the palette is exhausted.
	for (let index = 1; index <= 0xffffff; index++) {
		const candidate = `#${((index * 0x9e3779) & 0xffffff).toString(16).padStart(6, '0').toUpperCase()}`;
		if (!used.has(candidate)) return candidate;
	}
	return environmentColorPalette[0]!;
}

function savedColors(): Record<string, string> {
	return userStore.current?.preferences?.environmentColors ?? {};
}

/** Every environment's color: saved picks first, then unique defaults for the rest. */
function effectiveColors(saved: Record<string, string>): Map<string, string> {
	const colors = new Map<string, string>();
	const used = new Set<string>();
	for (const [id, color] of Object.entries(saved)) {
		const normalized = normalizeColor(color);
		if (used.has(normalized)) continue;
		colors.set(id, normalized);
		used.add(normalized);
	}
	const ids = environmentStore.available.map((env) => env.id).sort();
	for (const id of ids.filter((id) => !colors.has(id))) {
		const color = availableColor(used);
		colors.set(id, color);
		used.add(color);
	}
	return colors;
}

export function environmentColor(environmentId: string): string | undefined {
	return effectiveColors(savedColors()).get(environmentId);
}

/**
 * Colors to save after setting one environment's color, or clearing it with null so it falls back to a
 * default. Every other environment keeps the color it shows now. Returns null when another
 * environment already shows that color.
 */
export function withEnvironmentColor(environmentId: string, color: string | null): Record<string, string> | null {
	const others = effectiveColors(savedColors());
	others.delete(environmentId);
	if (color === null) return Object.fromEntries(others);
	const normalized = normalizeColor(color);
	if ([...others.values()].includes(normalized)) return null;
	return { ...Object.fromEntries(others), [environmentId]: normalized };
}
