import type { AuthenticationResponseJSON, RegistrationResponseJSON } from '@simplewebauthn/browser';

export const MOBILE_PASSKEY_BRIDGE_VERSION = 2;
export const MOBILE_PASSKEY_MAX_REQUEST_BYTES = 65_536;
export const MOBILE_PASSKEY_MAX_RESPONSE_BYTES = 65_536;

const CALLBACK_URL = 'arcane-mobile://passkey-callback';
const REQUEST_FRAGMENT_PARAMETER = 'request';
const STATE_BYTES = 32;
const BASE64URL_PATTERN = /^[A-Za-z0-9_-]+$/;

export type MobilePasskeyOperation = 'authenticate' | 'register';
export type MobilePasskeyErrorCode = 'invalid_request' | 'oversized' | 'unsupported' | 'cancelled' | 'failed';
export type MobilePasskeyCredential = AuthenticationResponseJSON | RegistrationResponseJSON;

export interface MobilePasskeyBridgeRequest {
	version: 2;
	state: string;
	operation: MobilePasskeyOperation;
	options: Record<string, unknown>;
	mobileLogin?: {
		ceremonyId: string;
		codeChallenge: string;
	};
}

export class MobilePasskeyBridgeRequestError extends Error {
	constructor(
		public readonly code: Extract<MobilePasskeyErrorCode, 'invalid_request' | 'oversized'>,
		public readonly state?: string
	) {
		super(code);
		this.name = 'MobilePasskeyBridgeRequestError';
	}
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function decodeBase64URL(value: string, maximumBytes: number): Uint8Array {
	if (!value || !BASE64URL_PATTERN.test(value)) {
		throw new Error('invalid_base64url');
	}

	const maximumEncodedLength = Math.ceil(maximumBytes / 3) * 4;
	if (value.length > maximumEncodedLength) {
		throw new MobilePasskeyBridgeRequestError('oversized');
	}

	const padding = '='.repeat((4 - (value.length % 4)) % 4);
	const decoded = atob(value.replaceAll('-', '+').replaceAll('_', '/') + padding);
	if (decoded.length > maximumBytes) {
		throw new MobilePasskeyBridgeRequestError('oversized');
	}

	return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

function isValidState(value: unknown): value is string {
	if (typeof value !== 'string') return false;
	try {
		return decodeBase64URL(value, STATE_BYTES).byteLength === STATE_BYTES;
	} catch {
		return false;
	}
}

function encodeBase64URL(value: Uint8Array): string {
	let binary = '';
	for (const byte of value) binary += String.fromCharCode(byte);
	return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
}

export function decodeMobilePasskeyBridgeRequest(fragment: string): MobilePasskeyBridgeRequest {
	const params = new URLSearchParams(fragment.startsWith('#') ? fragment.slice(1) : fragment);
	const requestValues = params.getAll(REQUEST_FRAGMENT_PARAMETER);
	if (requestValues.length !== 1 || [...params.keys()].some((key) => key !== REQUEST_FRAGMENT_PARAMETER)) {
		throw new MobilePasskeyBridgeRequestError('invalid_request');
	}

	let requestBytes: Uint8Array;
	try {
		requestBytes = decodeBase64URL(requestValues[0] ?? '', MOBILE_PASSKEY_MAX_REQUEST_BYTES);
	} catch (error) {
		if (error instanceof MobilePasskeyBridgeRequestError) throw error;
		throw new MobilePasskeyBridgeRequestError('invalid_request');
	}

	let candidate: unknown;
	try {
		candidate = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(requestBytes));
	} catch {
		throw new MobilePasskeyBridgeRequestError('invalid_request');
	}

	if (!isRecord(candidate)) {
		throw new MobilePasskeyBridgeRequestError('invalid_request');
	}

	const state = isValidState(candidate['state']) ? candidate['state'] : undefined;
	const keys = Object.keys(candidate).sort();
	const mobileLogin = candidate['mobileLogin'];
	const hasExactShape =
		(keys.length === 4 && keys[0] === 'operation' && keys[1] === 'options' && keys[2] === 'state' && keys[3] === 'version') ||
		(keys.length === 5 &&
			keys[0] === 'mobileLogin' &&
			keys[1] === 'operation' &&
			keys[2] === 'options' &&
			keys[3] === 'state' &&
			keys[4] === 'version');
	const operation = candidate['operation'];
	const hasValidMobileLogin =
		mobileLogin === undefined ||
		(isRecord(mobileLogin) &&
			Object.keys(mobileLogin).length === 2 &&
			typeof mobileLogin['ceremonyId'] === 'string' &&
			mobileLogin['ceremonyId'].length > 0 &&
			mobileLogin['ceremonyId'].length <= 128 &&
			typeof mobileLogin['codeChallenge'] === 'string' &&
			/^[A-Za-z0-9_-]{43}$/.test(mobileLogin['codeChallenge']));

	if (
		!hasExactShape ||
		candidate['version'] !== MOBILE_PASSKEY_BRIDGE_VERSION ||
		!state ||
		(operation !== 'authenticate' && operation !== 'register') ||
		!isRecord(candidate['options']) ||
		!hasValidMobileLogin ||
		(mobileLogin !== undefined && operation !== 'authenticate')
	) {
		throw new MobilePasskeyBridgeRequestError('invalid_request', state);
	}

	return {
		version: MOBILE_PASSKEY_BRIDGE_VERSION,
		state,
		operation,
		options: candidate['options'],
		...(mobileLogin === undefined
			? {}
			: {
					mobileLogin: {
						ceremonyId: mobileLogin['ceremonyId'] as string,
						codeChallenge: mobileLogin['codeChallenge'] as string
					}
				})
	};
}

export function makeMobilePasskeySuccessCallback(state: string, credential: MobilePasskeyCredential): string {
	const responseBytes = new TextEncoder().encode(JSON.stringify(credential));
	if (responseBytes.byteLength > MOBILE_PASSKEY_MAX_RESPONSE_BYTES) {
		throw new Error('oversized_response');
	}

	const callback = new URL(CALLBACK_URL);
	callback.searchParams.set('state', state);
	callback.searchParams.set('response', encodeBase64URL(responseBytes));
	return callback.toString();
}

export function makeMobilePasskeyLoginCallback(state: string, transactionId: string): string {
	const callback = new URL(CALLBACK_URL);
	callback.searchParams.set('state', state);
	callback.searchParams.set('transaction', transactionId);
	return callback.toString();
}

export function makeMobilePasskeyErrorCallback(state: string, code: MobilePasskeyErrorCode): string {
	const callback = new URL(CALLBACK_URL);
	callback.searchParams.set('state', state);
	callback.searchParams.set('error', code);
	return callback.toString();
}

export function classifyMobilePasskeyError(error: unknown): MobilePasskeyErrorCode {
	if (
		error instanceof Error &&
		(error.name === 'NotAllowedError' || error.name === 'AbortError' || Reflect.get(error, 'code') === 'ERROR_CEREMONY_ABORTED')
	) {
		return 'cancelled';
	}
	return 'failed';
}
