import { m } from '#lib/paraglide/messages.js';
import type { User } from '#lib/types/user.js';

export type BackdropPreset = {
	id: string;
	label: () => string;
	photographer: string;
	photographerUrl: string;
};

export const BACKDROP_NONE = 'none';
export const BACKDROP_CUSTOM = 'custom';
export const BACKDROP_DEFAULT_DIM = 45;
export const BACKDROP_MAX_UPLOAD_BYTES = 10 * 1024 * 1024;
export const BACKDROP_UPLOAD_TYPES = ['image/png', 'image/jpeg', 'image/webp'];

// Free photos under the Unsplash License (https://unsplash.com/license).
export const backdropPresets: BackdropPreset[] = [
	{
		id: 'ribbon',
		label: m.ribbon,
		photographer: 'Pawel Czerwinski',
		photographerUrl: 'https://unsplash.com/@pawel_czerwinski'
	},
	{
		id: 'dusk',
		label: m.dusk,
		photographer: 'Pawel Czerwinski',
		photographerUrl: 'https://unsplash.com/@pawel_czerwinski'
	},
	{
		id: 'ember',
		label: m.ember,
		photographer: 'Milad Fakurian',
		photographerUrl: 'https://unsplash.com/@fakurian'
	},
	{
		id: 'silver',
		label: m.silver,
		photographer: 'Milad Fakurian',
		photographerUrl: 'https://unsplash.com/@fakurian'
	}
];

export function backdropPresetImage(id: string): string {
	return `/backdrops/${id}.avif`;
}

export function backdropPresetThumbnail(id: string): string {
	return `/backdrops/${id}-thumb.avif`;
}

/** The image the user's backdrop preference points to, or undefined for none. */
export function resolveBackdropSource(user: User | null | undefined): string | undefined {
	const backdrop = user?.preferences?.backdrop ?? BACKDROP_NONE;
	if (backdrop === BACKDROP_CUSTOM) return user?.backdropUrl;
	if (backdropPresets.some((preset) => preset.id === backdrop)) return backdropPresetImage(backdrop);
	return undefined;
}
