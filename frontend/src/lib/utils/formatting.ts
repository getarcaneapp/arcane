import Convert from 'ansi-to-html';
import { Temporal } from 'temporal-polyfill';
import { z } from 'zod/v4';
import { getLocale, setLocale as setParaglideLocale, type Locale } from '#lib/paraglide/runtime.js';
import { timeFormatStore } from '#lib/stores/time-format.store.svelte.js';

// --- String helpers ---

export function capitalizeFirstLetter(string: string): string {
	if (!string) return '';
	return string.charAt(0).toUpperCase() + string.slice(1);
}

export function truncateString(str: string | undefined, maxLength: number): string {
	if (!str) return '';
	if (str.length <= maxLength) {
		return str;
	}
	return str.substring(0, maxLength - 3) + '...';
}

export function truncateImageDigest(image: string): string {
	return image.replace(/@sha256:([a-f0-9]{7})[a-f0-9]+/g, '@sha256:$1');
}

// --- Byte formatting (MIT, TJ Holowaychuk / Jed Watson) ---

export type BytesFormatOptions = {
	decimalPlaces?: number;
	fixedDecimals?: boolean;
	thousandsSeparator?: string;
	unit?: string;
	unitSeparator?: string;
};

export type BytesValue = string | number;

const formatThousandsRegExp = /\B(?=(\d{3})+(?!\d))/g;
const formatDecimalsRegExp = /(?:\.0*|(\.[^0]+)0+)$/;

const bytesUnitMap = {
	b: 1,
	kb: 1 << 10,
	mb: 1 << 20,
	gb: 1 << 30,
	tb: Math.pow(1024, 4),
	pb: Math.pow(1024, 5)
} as const;

const parseBytesRegExp = /^((-|\+)?(\d+(?:\.\d+)?)) *(kb|mb|gb|tb|pb)$/i;

type BytesFunction = {
	(value: string, options?: BytesFormatOptions): number | null;
	(value: number, options?: BytesFormatOptions): string | null;
	(value: BytesValue, options?: BytesFormatOptions): string | number | null;
	format: typeof formatBytes;
	parse: typeof parseBytes;
};

function bytesImpl(value: string, options?: BytesFormatOptions): number | null;
function bytesImpl(value: number, options?: BytesFormatOptions): string | null;
function bytesImpl(value: BytesValue, options?: BytesFormatOptions): string | number | null {
	if (typeof value === 'string') {
		return parseBytes(value);
	}

	if (typeof value === 'number') {
		return formatBytes(value, options);
	}

	return null;
}

function formatBytes(value: number, options?: BytesFormatOptions): string | null {
	if (!Number.isFinite(value)) {
		return null;
	}

	const magnitude = Math.abs(value);
	const thousandsSeparator = options?.thousandsSeparator ?? '';
	const unitSeparator = options?.unitSeparator ?? '';
	const decimalPlaces = options?.decimalPlaces !== undefined ? options.decimalPlaces : 2;
	const fixedDecimals = Boolean(options?.fixedDecimals);
	let unit = options?.unit ?? '';

	const normalizedUnit = unit.toLowerCase() as keyof typeof bytesUnitMap;
	if (!unit || !bytesUnitMap[normalizedUnit]) {
		unit =
			(Object.keys(bytesUnitMap) as (keyof typeof bytesUnitMap)[])
				.findLast((candidate) => magnitude >= bytesUnitMap[candidate])
				?.toUpperCase() ?? 'B';
	}

	const divisor = bytesUnitMap[unit.toLowerCase() as keyof typeof bytesUnitMap];
	const val = value / divisor;
	let str = val.toFixed(decimalPlaces);

	if (!fixedDecimals) {
		str = str.replace(formatDecimalsRegExp, '$1');
	}

	if (thousandsSeparator) {
		str = str
			.split('.')
			.map((part, index) => (index === 0 ? part.replace(formatThousandsRegExp, thousandsSeparator) : part))
			.join('.');
	}

	return str + unitSeparator + unit;
}

function parseBytes(val: string | number): number | null {
	if (typeof val === 'number' && !Number.isNaN(val)) {
		return val;
	}

	if (typeof val !== 'string') {
		return null;
	}

	const results = parseBytesRegExp.exec(val);
	let floatValue: number;
	let unit: keyof typeof bytesUnitMap;

	if (!results) {
		floatValue = Number.parseInt(val, 10);
		unit = 'b';
	} else {
		const numericValue = results[1];
		const matchedUnit = results[4];
		if (!numericValue || !matchedUnit) {
			return null;
		}

		floatValue = Number.parseFloat(numericValue);
		unit = matchedUnit.toLowerCase() as keyof typeof bytesUnitMap;
	}

	if (Number.isNaN(floatValue)) {
		return null;
	}

	return Math.floor(bytesUnitMap[unit] * floatValue);
}

const bytesWithHelpers = bytesImpl as BytesFunction;
bytesWithHelpers.format = formatBytes;
bytesWithHelpers.parse = parseBytes;

export const bytes = bytesWithHelpers;

// --- Locale-aware date/time formatting ---

export type InstantInput = Temporal.Instant | string | null | undefined;

type DateDisplayStyle = 'short' | 'medium' | 'month-day';

type AbsoluteDateTimeFormatOptions = {
	dateStyle?: DateDisplayStyle;
	includeSeconds?: boolean;
	timeZone?: string;
};

type RelativeTimeFormatOptions = {
	base?: InstantInput;
};

type RelativeTimeUnit = 'second' | 'minute' | 'hour' | 'day' | 'month' | 'year';

type RelativeTimeValue = {
	unit: RelativeTimeUnit;
	value: number;
};

const dateTimeFormatterCache = new Map<string, Intl.DateTimeFormat>();
const relativeTimeFormatterCache = new Map<string, Intl.RelativeTimeFormat>();
const elapsedTimeFormatterCache = new Map<string, Intl.NumberFormat>();

const dateFormatOptions: Record<DateDisplayStyle, Intl.DateTimeFormatOptions> = {
	short: { day: 'numeric', month: 'numeric', year: 'numeric' },
	medium: { day: 'numeric', month: 'short', year: 'numeric' },
	'month-day': { day: 'numeric', month: 'short' }
};

export function parseInstant(value: InstantInput): Temporal.Instant | null {
	if (!value) return null;
	if (typeof value !== 'string') return value;

	const normalized = value.trim();
	// Docker zero-values missing timestamps as 0001-01-01T00:00:00Z.
	if (!normalized || normalized.startsWith('0001-01-01')) return null;

	try {
		return Temporal.Instant.from(normalized);
	} catch {
		// Third-party-shaped timestamps (offset-less, date-only, human-readable)
		// that strict ISO parsing rejects but Date historically accepted.
		const epochMs = Date.parse(normalized);
		return Number.isNaN(epochMs) ? null : Temporal.Instant.fromEpochMilliseconds(epochMs);
	}
}

export function instantEpochMilliseconds(value: InstantInput): number | null {
	return parseInstant(value)?.epochMilliseconds ?? null;
}

export function nowInstantString(): string {
	return Temporal.Now.instant().toString({ smallestUnit: 'millisecond' });
}

// The polyfill's Temporal.Now.timeZoneId() constructs a fresh Intl.DateTimeFormat
// on every call, so resolve the local zone once per session.
const localTimeZoneId = Temporal.Now.timeZoneId();

export function plainDateFromInstant(value: InstantInput, timeZone = localTimeZoneId): Temporal.PlainDate | undefined {
	return parseInstant(value)?.toZonedDateTimeISO(timeZone).toPlainDate();
}

export function plainDateToInstantString(value: Temporal.PlainDate, timeZone = localTimeZoneId): string {
	return value.toZonedDateTime({ timeZone, plainTime: '00:00' }).toInstant().toString({ smallestUnit: 'millisecond' });
}

function dateTimeFormatterInternal(
	dateStyle: DateDisplayStyle | undefined,
	includeTime: boolean,
	includeSeconds: boolean,
	timeZone: string
): Intl.DateTimeFormat {
	const locale = getLocale();
	const timeFormat = timeFormatStore.current;
	const key = `${locale}|${timeZone}|${dateStyle ?? 'none'}|${includeTime}|${includeSeconds}|${timeFormat}`;
	const cached = dateTimeFormatterCache.get(key);
	if (cached) return cached;

	const options: Intl.DateTimeFormatOptions = {
		...(dateStyle ? dateFormatOptions[dateStyle] : {}),
		timeZone
	};

	if (includeTime) {
		options.hour = 'numeric';
		options.minute = '2-digit';
		if (includeSeconds) options.second = '2-digit';
		if (timeFormat === '12h') options.hour12 = true;
		if (timeFormat === '24h') options.hour12 = false;
	}

	const formatter = new Intl.DateTimeFormat(locale, options);
	dateTimeFormatterCache.set(key, formatter);
	return formatter;
}

function formatAbsoluteDateTimeInternal(
	value: InstantInput,
	options: AbsoluteDateTimeFormatOptions,
	includeTime: boolean
): string {
	const instant = parseInstant(value);
	if (!instant) return '';

	const timeZone = options.timeZone ?? localTimeZoneId;
	const formatter = dateTimeFormatterInternal(options.dateStyle, includeTime, options.includeSeconds ?? true, timeZone);
	return formatter.format(instant.epochMilliseconds);
}

export function formatDate(
	value: InstantInput,
	options: Pick<AbsoluteDateTimeFormatOptions, 'dateStyle' | 'timeZone'> = { dateStyle: 'medium' }
): string {
	return formatAbsoluteDateTimeInternal(value, { ...options, dateStyle: options.dateStyle ?? 'medium' }, false);
}

export function formatDateTime(
	value: InstantInput,
	options: AbsoluteDateTimeFormatOptions = { dateStyle: 'medium', includeSeconds: true }
): string {
	return formatAbsoluteDateTimeInternal(value, { ...options, dateStyle: options.dateStyle ?? 'medium' }, true);
}

export function formatDateTimeShort(value: InstantInput): string {
	return formatAbsoluteDateTimeInternal(value, { dateStyle: 'medium', includeSeconds: false }, true);
}

export function formatOptionalDateTime(value: InstantInput, fallback = '-'): string {
	if (!value) return fallback;
	return formatDateTime(value) || fallback;
}

export function isPastDate(value: InstantInput): boolean {
	const instant = parseInstant(value);
	return instant ? Temporal.Instant.compare(instant, Temporal.Now.instant()) < 0 : false;
}

export function formatTime(value: InstantInput): string {
	return formatAbsoluteDateTimeInternal(value, { includeSeconds: true }, true);
}

function relativeTimeValueInternal(target: Temporal.Instant, base: Temporal.Instant): RelativeTimeValue {
	const deltaSeconds = (target.epochMilliseconds - base.epochMilliseconds) / 1000;
	const magnitude = Math.abs(deltaSeconds);
	const direction = Math.sign(deltaSeconds) || 1;
	// Math.round rounds halves toward +Infinity, which would make past deltas
	// round differently than future ones; round the magnitude and reapply the sign.
	const round = (value: number) => direction * Math.round(magnitude / value);

	// Past sub-30s deltas read as "now"; future ones keep their direction so a
	// pending run never looks like it already happened.
	if (magnitude < 30) return { unit: 'second', value: direction > 0 ? Math.max(1, Math.round(magnitude)) : 0 };
	if (magnitude < 90) return { unit: 'minute', value: direction };
	if (magnitude < 45 * 60) return { unit: 'minute', value: round(60) };
	if (magnitude < 90 * 60) return { unit: 'hour', value: direction };
	if (magnitude < 22 * 60 * 60) return { unit: 'hour', value: round(60 * 60) };
	if (magnitude < 36 * 60 * 60) return { unit: 'day', value: direction };
	if (magnitude < 26 * 24 * 60 * 60) return { unit: 'day', value: round(24 * 60 * 60) };
	if (magnitude < 45 * 24 * 60 * 60) return { unit: 'month', value: direction };
	if (magnitude < 320 * 24 * 60 * 60) {
		return { unit: 'month', value: round(30.4375 * 24 * 60 * 60) };
	}
	if (magnitude < 548 * 24 * 60 * 60) return { unit: 'year', value: direction };
	return { unit: 'year', value: round(365.2425 * 24 * 60 * 60) };
}

function relativeTimeFormatterInternal(numeric: 'always' | 'auto'): Intl.RelativeTimeFormat {
	const locale = getLocale();
	const key = `${locale}|${numeric}`;
	const cached = relativeTimeFormatterCache.get(key);
	if (cached) return cached;

	const formatter = new Intl.RelativeTimeFormat(locale, { numeric });
	relativeTimeFormatterCache.set(key, formatter);
	return formatter;
}

function relativeTimeContextInternal(value: InstantInput, options: RelativeTimeFormatOptions) {
	const target = parseInstant(value);
	const base = options.base ? parseInstant(options.base) : Temporal.Now.instant();
	if (!target || !base) return null;
	return { target, base, relative: relativeTimeValueInternal(target, base) };
}

export function formatRelativeTime(value: InstantInput, options: RelativeTimeFormatOptions = {}): string {
	const context = relativeTimeContextInternal(value, options);
	if (!context) return '';
	const { relative } = context;
	const numeric = relative.value === 0 ? 'auto' : 'always';
	return relativeTimeFormatterInternal(numeric).format(relative.value, relative.unit);
}

function elapsedTimeFormatterInternal(unit: RelativeTimeUnit): Intl.NumberFormat {
	const locale = getLocale();
	const key = `${locale}|${unit}`;
	const cached = elapsedTimeFormatterCache.get(key);
	if (cached) return cached;

	const formatter = new Intl.NumberFormat(locale, {
		style: 'unit',
		unit,
		unitDisplay: 'long'
	});
	elapsedTimeFormatterCache.set(key, formatter);
	return formatter;
}

export function formatElapsedTime(value: InstantInput, options: RelativeTimeFormatOptions = {}): string {
	const context = relativeTimeContextInternal(value, options);
	if (!context) return '';
	const { target, base, relative } = context;
	const elapsedValue =
		relative.value === 0
			? Math.round(Math.abs(target.epochMilliseconds - base.epochMilliseconds) / 1000)
			: Math.abs(relative.value);
	return elapsedTimeFormatterInternal(relative.unit).format(elapsedValue);
}

export async function setLocale(locale: Locale, reload = true) {
	try {
		const zodLocale = await import(`../../../node_modules/zod/v4/locales/${locale}.js`);
		z.config(zodLocale.default());
	} catch (error) {
		console.warn(`Failed to load zod locale for ${locale}:`, error);
	}

	setParaglideLocale(locale, { reload });
}

// --- ANSI conversion ---

const ansiConverter = new Convert({
	fg: '#e4e4e7',
	bg: '#000000',
	newline: false,
	escapeXML: true,
	stream: false,
	colors: {
		0: '#18181b',
		1: '#ef4444',
		2: '#22c55e',
		3: '#eab308',
		4: '#3b82f6',
		5: '#a855f7',
		6: '#06b6d4',
		7: '#f4f4f5',
		8: '#71717a',
		9: '#f87171',
		10: '#4ade80',
		11: '#facc15',
		12: '#60a5fa',
		13: '#c084fc',
		14: '#22d3ee',
		15: '#fafafa'
	}
});

export function ansiToHtml(text: string): string {
	if (!text) return '';
	return ansiConverter.toHtml(text);
}

// --- Log text sanitization ---

const ANSI_ESCAPE_SEQUENCE = /\x1B\[[0-?]*[ -/]*[@-~]/g;
const ANSI_OSC_SEQUENCE = /\x1B\][^\x07]*(?:\x07|\x1B\\)/g;
const LOOSE_ANSI_MARKER_SEQUENCE = /\[(?:\d{1,3}(?:;\d{1,3})*)m/g;

function stripAnsi(input: string): string {
	return input.replace(ANSI_ESCAPE_SEQUENCE, '').replace(ANSI_OSC_SEQUENCE, '').replace(LOOSE_ANSI_MARKER_SEQUENCE, '');
}

export function sanitizeLogText(input: string): string {
	return stripAnsi(input.replace(/\r/g, '')).trimEnd();
}

// --- Email validation ---

const EMAIL_LOCAL_PART_PATTERN = /^[A-Za-z0-9!#$%&'*+/=?^_`{|}~.-]+$/;
const EMAIL_DOMAIN_LABEL_PATTERN = /^[\p{L}\p{N}](?:[\p{L}\p{N}-]{0,61}[\p{L}\p{N}])?$/u;

export function isValidUserEmail(email: string): boolean {
	const trimmedEmail = email.trim();
	if (!trimmedEmail || trimmedEmail.includes(' ')) {
		return false;
	}

	const atIndex = trimmedEmail.indexOf('@');
	if (atIndex <= 0 || atIndex !== trimmedEmail.lastIndexOf('@') || atIndex === trimmedEmail.length - 1) {
		return false;
	}

	const localPart = trimmedEmail.slice(0, atIndex);
	const domainPart = trimmedEmail.slice(atIndex + 1);

	return isValidLocalPart(localPart) && isValidDomainPart(domainPart);
}

function isValidLocalPart(localPart: string): boolean {
	if (!localPart || localPart.length > 64 || localPart.startsWith('.') || localPart.endsWith('.') || localPart.includes('..')) {
		return false;
	}

	return EMAIL_LOCAL_PART_PATTERN.test(localPart);
}

function isValidDomainPart(domainPart: string): boolean {
	if (!domainPart || domainPart.length > 255) {
		return false;
	}

	if (isValidIPv4Literal(domainPart)) {
		return true;
	}

	if (isValidIPv6Literal(domainPart)) {
		return true;
	}

	const labels = domainPart.split('.');
	if (labels.length === 4 && labels.every((label) => /^\d+$/.test(label))) {
		return false;
	}

	if (labels.some((label) => !EMAIL_DOMAIN_LABEL_PATTERN.test(label))) {
		return false;
	}

	return true;
}

function isValidIPv4Literal(domainPart: string): boolean {
	const octets = domainPart.split('.');
	if (octets.length !== 4) {
		return false;
	}

	return octets.every((octet) => /^\d+$/.test(octet) && Number(octet) >= 0 && Number(octet) <= 255);
}

function isValidIPv6Literal(domainPart: string): boolean {
	if (!/^\[IPv6:[0-9A-Fa-f:.]+\]$/i.test(domainPart)) {
		return false;
	}

	const address = domainPart.slice(6, -1);
	return isValidIPv6Address(address);
}

function isValidIPv6Address(address: string): boolean {
	if (!address.includes(':') || address.includes(':::')) {
		return false;
	}

	const compressionIndex = address.indexOf('::');
	if (compressionIndex !== -1 && compressionIndex !== address.lastIndexOf('::')) {
		return false;
	}

	if (compressionIndex === -1) {
		return countIPv6Segments(address.split(':')) === 8;
	}

	const [left = '', right = ''] = address.split('::');
	const leftCount = left ? countIPv6Segments(left.split(':')) : 0;
	const rightCount = right ? countIPv6Segments(right.split(':')) : 0;

	return leftCount >= 0 && rightCount >= 0 && leftCount + rightCount < 8;
}

function countIPv6Segments(segments: string[]): number {
	let count = 0;

	for (let i = 0; i < segments.length; i += 1) {
		const segment = segments[i];
		if (!segment) {
			return -1;
		}

		const isLastSegment = i === segments.length - 1;
		if (segment.includes('.')) {
			return isLastSegment && isValidIPv4Literal(segment) ? count + 2 : -1;
		}

		if (!/^[0-9A-Fa-f]{1,4}$/.test(segment)) {
			return -1;
		}

		count += 1;
	}

	return count;
}

// --- Browser file download ---

export function downloadTextFile(filename: string, content: string, mimeType = 'application/x-pem-file'): void {
	const blob = new Blob([content], { type: `${mimeType};charset=utf-8` });
	const url = window.URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = url;
	link.setAttribute('download', filename);
	document.body.appendChild(link);
	link.click();
	document.body.removeChild(link);
	window.URL.revokeObjectURL(url);
}
