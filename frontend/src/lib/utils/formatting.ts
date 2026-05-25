import Convert from 'ansi-to-html';
import { format as formatDate, setDefaultOptions } from 'date-fns';
import { z } from 'zod/v4';
import { setLocale as setParaglideLocale, type Locale } from '$lib/paraglide/runtime';

// --- String helpers ---

export function capitalizeFirstLetter(string: string): string {
	if (!string) return '';
	return string.charAt(0).toUpperCase() + string.slice(1);
}

export function shortId(id: string | undefined, length = 12): string {
	if (!id) return 'N/A';
	return id.substring(0, length);
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

export function formatBytes(value: number, options?: BytesFormatOptions): string | null {
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
		if (magnitude >= bytesUnitMap.pb) {
			unit = 'PB';
		} else if (magnitude >= bytesUnitMap.tb) {
			unit = 'TB';
		} else if (magnitude >= bytesUnitMap.gb) {
			unit = 'GB';
		} else if (magnitude >= bytesUnitMap.mb) {
			unit = 'MB';
		} else if (magnitude >= bytesUnitMap.kb) {
			unit = 'KB';
		} else {
			unit = 'B';
		}
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

export function parseBytes(val: string | number): number | null {
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

export function formatDateTime(date: Date | string | null | undefined): string {
	if (!date) return '';
	const d = typeof date === 'string' ? new Date(date) : date;
	if (isNaN(d.getTime())) return '';
	return formatDate(d, 'PPpp');
}

export function formatDateTimeShort(date: Date | string | null | undefined): string {
	if (!date) return '';
	const d = typeof date === 'string' ? new Date(date) : date;
	if (isNaN(d.getTime())) return '';
	return formatDate(d, 'PPp');
}

export function formatTime(date: Date | string | null | undefined): string {
	if (!date) return '';
	const d = typeof date === 'string' ? new Date(date) : date;
	if (isNaN(d.getTime())) return '';
	return formatDate(d, 'pp');
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

export function stripAnsi(input: string): string {
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
