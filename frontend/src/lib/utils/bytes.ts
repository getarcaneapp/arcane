/*!
 * bytes
 * Copyright(c) 2012-2014 TJ Holowaychuk
 * Copyright(c) 2015 Jed Watson
 * MIT Licensed
 *
 * TypeScript adaptation for Arcane.
 */

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

const map = {
	b: 1,
	kb: 1 << 10,
	mb: 1 << 20,
	gb: 1 << 30,
	tb: Math.pow(1024, 4),
	pb: Math.pow(1024, 5)
} as const;

const parseRegExp = /^((-|\+)?(\d+(?:\.\d+)?)) *(kb|mb|gb|tb|pb)$/i;

type BytesFunction = {
	(value: string, options?: BytesFormatOptions): number | null;
	(value: number, options?: BytesFormatOptions): string | null;
	(value: BytesValue, options?: BytesFormatOptions): string | number | null;
	format: typeof format;
	parse: typeof parse;
};

function bytes(value: string, options?: BytesFormatOptions): number | null;
function bytes(value: number, options?: BytesFormatOptions): string | null;
function bytes(value: BytesValue, options?: BytesFormatOptions): string | number | null {
	if (typeof value === 'string') {
		return parse(value);
	}

	if (typeof value === 'number') {
		return format(value, options);
	}

	return null;
}

export function format(value: number, options?: BytesFormatOptions): string | null {
	if (!Number.isFinite(value)) {
		return null;
	}

	const magnitude = Math.abs(value);
	const thousandsSeparator = options?.thousandsSeparator ?? '';
	const unitSeparator = options?.unitSeparator ?? '';
	const decimalPlaces = options?.decimalPlaces !== undefined ? options.decimalPlaces : 2;
	const fixedDecimals = Boolean(options?.fixedDecimals);
	let unit = options?.unit ?? '';

	const normalizedUnit = unit.toLowerCase() as keyof typeof map;
	if (!unit || !map[normalizedUnit]) {
		if (magnitude >= map.pb) {
			unit = 'PB';
		} else if (magnitude >= map.tb) {
			unit = 'TB';
		} else if (magnitude >= map.gb) {
			unit = 'GB';
		} else if (magnitude >= map.mb) {
			unit = 'MB';
		} else if (magnitude >= map.kb) {
			unit = 'KB';
		} else {
			unit = 'B';
		}
	}

	const divisor = map[unit.toLowerCase() as keyof typeof map];
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

export function parse(val: string | number): number | null {
	if (typeof val === 'number' && !Number.isNaN(val)) {
		return val;
	}

	if (typeof val !== 'string') {
		return null;
	}

	const results = parseRegExp.exec(val);
	let floatValue: number;
	let unit: keyof typeof map = 'b';

	if (!results) {
		floatValue = Number.parseInt(val, 10);
		unit = 'b';
	} else {
		floatValue = Number.parseFloat(results[1]);
		unit = results[4].toLowerCase() as keyof typeof map;
	}

	if (Number.isNaN(floatValue)) {
		return null;
	}

	return Math.floor(map[unit] * floatValue);
}

const bytesWithHelpers = bytes as BytesFunction;
bytesWithHelpers.format = format;
bytesWithHelpers.parse = parse;

export default bytesWithHelpers;
