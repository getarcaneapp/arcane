import { setLocale as setParaglideLocale, type Locale } from '$lib/paraglide/runtime';
import { format, setDefaultOptions } from 'date-fns';
import { z } from 'zod/v4';

/**
 * Format a date/timestamp as a locale-aware datetime string.
 * Uses date-fns which picks up the locale set by setLocale(), so it
 * reflects the language the user has chosen in the app rather than
 * defaulting to the browser's system locale.
 */
export function formatDateTime(date: Date | string | null | undefined): string {
	if (!date) return '';
	const d = typeof date === 'string' ? new Date(date) : date;
	if (isNaN(d.getTime())) return '';
	return format(d, 'PPpp');
}

/**
 * Same as formatDateTime but omits seconds (PPp).
 */
export function formatDateTimeShort(date: Date | string | null | undefined): string {
	if (!date) return '';
	const d = typeof date === 'string' ? new Date(date) : date;
	if (isNaN(d.getTime())) return '';
	return format(d, 'PPp');
}

export async function setLocale(locale: Locale, reload = true) {
	let dateFnsLocale: string = locale;
	if (dateFnsLocale === 'en') {
		dateFnsLocale = 'en-US';
	}

	const [zodResult, dateFnsResult] = await Promise.allSettled([
		import(`../../../node_modules/zod/v4/locales/${locale}.js`),
		import(`../../../node_modules/date-fns/locale/${dateFnsLocale}.js`)
	]);

	if (zodResult.status === 'fulfilled') {
		z.config(zodResult.value.default());
	} else {
		console.warn(`Failed to load zod locale for ${locale}:`, zodResult.reason);
	}

	setParaglideLocale(locale, { reload });

	if (dateFnsResult.status === 'fulfilled') {
		setDefaultOptions({
			locale: dateFnsResult.value.default
		});
	} else {
		console.warn(`Failed to load date-fns locale for ${locale}:`, dateFnsResult.reason);
	}
}
