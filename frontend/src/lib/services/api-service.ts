import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { downloadFromUrl } from '#lib/utils/browser-download.js';
import { tryCatch } from '#lib/utils/try-catch.js';

const errorBodyTimeoutMs = 10_000;
const maxErrorBodyBytes = 10 * 1024 * 1024;

type SearchParams = URLSearchParams | Record<string, string | number | boolean | undefined>;

export interface APIRequestConfig {
	baseURL?: string;
	cache?: RequestCache;
	data?: unknown;
	headers?: HeadersInit;
	params?: SearchParams;
	responseType?: 'json' | 'text' | 'blob' | 'arrayBuffer';
	signal?: AbortSignal;
	suppressAccessDeniedToast?: boolean;
	timeout?: number | false;
	onFailedEnvironments?: (failures: { environment: { id: string; name: string }; message: string }[]) => void;
	onEnvironments?: (entries: { environment: { id: string; name: string }; data: unknown }[]) => void;
}

interface InternalRequestConfig extends APIRequestConfig {
	_retry?: boolean;
}

export interface APIResponse<T = unknown> {
	data: T;
	headers: Headers;
	raw: Response;
	status: number;
}

// Thrown for work belonging to a session that ended through logout.
export class SessionCancelledError extends Error {
	constructor() {
		super('Session ended');
		this.name = 'SessionCancelledError';
	}
}

// Browsers may surface a logout abort as a plain AbortError instead of the abort reason.
export function isSessionCancelledError(error: unknown): boolean {
	if (error instanceof SessionCancelledError) return true;
	return error instanceof Error && error.name === 'AbortError' && sessionController.signal.aborted;
}

export class APIError extends Error {
	config: InternalRequestConfig & { method?: string; url?: string };
	request?: { url: string };
	response?: APIResponse;
	status?: number;

	constructor(
		message: string,
		options: {
			config: InternalRequestConfig & { method?: string; url?: string };
			name?: string;
			requestUrl?: string;
			response?: APIResponse;
			cause?: unknown;
		}
	) {
		super(message, { cause: options.cause });
		this.name = options.name ?? 'APIError';
		this.config = options.config;
		this.request = options.requestUrl ? { url: options.requestUrl } : undefined;
		this.response = options.response;
		this.status = options.response?.status;
	}
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
	return value !== null && typeof value === 'object' ? (value as Record<string, unknown>) : undefined;
}

function nonEmptyString(value: unknown): string | undefined {
	return typeof value === 'string' && value.trim() ? value : undefined;
}

export function extractServerMessage(data: unknown, includeErrors = false): string | undefined {
	const innerValue = asRecord(data)?.['data'] ?? data;
	const stringValue = nonEmptyString(innerValue);
	if (stringValue) return stringValue;

	const inner = asRecord(innerValue);
	if (!inner) return undefined;
	if (inner['code'] === 'feature_disabled' && inner['feature'] === 'vulnerabilityManagement') {
		return m.features_vulnerability_disabled();
	}

	for (const key of ['error', 'message', 'detail', 'error_description']) {
		const message = nonEmptyString(inner[key]);
		if (message) return message;
	}

	if (!includeErrors || !Array.isArray(inner['errors'])) return undefined;
	const first: unknown = inner['errors'][0];
	const firstError = asRecord(first);
	return nonEmptyString(first) ?? nonEmptyString(firstError?.['message']) ?? nonEmptyString(firstError?.['error']);
}

// Batch id for mutating requests started synchronously inside runWithActivityBatchId; it never spans an await,
// so fn must start its requests synchronously and return their promises.
let activeActivityBatchId: string | null = null;

export function runWithActivityBatchId<T>(batchId: string, fn: () => T): T {
	activeActivityBatchId = batchId;
	try {
		return fn();
	} finally {
		activeActivityBatchId = null;
	}
}

class HTTPStatusError extends Error {
	response: Response;

	constructor(response: Response) {
		super(`Request failed with status code ${response.status} ${response.statusText}`.trim());
		this.name = 'HTTPError';
		this.response = response;
	}
}

function withSearchParams(url: string, params?: SearchParams): string {
	if (!params) return url;
	let base = url;
	let hash = '';
	const hashIndex = url.indexOf('#');
	if (hashIndex !== -1) {
		base = url.slice(0, hashIndex);
		hash = url.slice(hashIndex);
	}
	let path = base;
	let existing = '';
	const queryIndex = base.indexOf('?');
	if (queryIndex !== -1) {
		path = base.slice(0, queryIndex);
		existing = base.slice(queryIndex + 1);
	}
	const search = new URLSearchParams(existing);
	const entries: [string, string | number | boolean | undefined][] =
		params instanceof URLSearchParams ? [...params] : Object.entries(params);
	for (const [key] of entries) search.delete(key);
	for (const [key, value] of entries) {
		if (value !== undefined) search.append(key, String(value));
	}
	const query = search.toString();
	return query ? `${path}?${query}${hash}` : `${path}${hash}`;
}

async function parseResponseBody(response: Response, responseType: APIRequestConfig['responseType'] = 'json'): Promise<unknown> {
	if (responseType === 'blob') {
		return response.blob();
	}
	if (responseType === 'text') {
		return response.text();
	}
	if (responseType === 'arrayBuffer') {
		return response.arrayBuffer();
	}
	if (response.status === 204 || response.status === 205) {
		return undefined;
	}

	const text = await response.text();
	if (!text) {
		return undefined;
	}

	try {
		return JSON.parse(text);
	} catch {
		return text;
	}
}

let tokenRefreshHandler: (() => Promise<string | null>) | null = null;
// True while a manager self-update or "Update All" runs; its version-mismatch 401s
// are recoverable and must not bounce the user to /login.
let upgradeInProgress = false;
let reloadForNewBuildStarted = false;
let unauthorizedRedirectStarted = false;
const skipAuthPaths = [
	'/auth/login',
	'/auth/logout',
	'/auth/refresh',
	'/auth/oidc',
	'/auth/passkey',
	'/auth/mfa',
	'/auth/autologin',
	'/settings/public'
];

// Each session owns one controller; logout aborts it and keeps it aborted until
// the next sign-in, so protected requests fail fast in between.
let sessionController = new AbortController();

/** Combines a caller signal with the current session, so logout cancels the request. */
export function withSessionSignal(signal?: AbortSignal | null): AbortSignal {
	const session = sessionController.signal;
	return signal ? AbortSignal.any([signal, session]) : session;
}

export function currentSessionSignal(): AbortSignal {
	return sessionController.signal;
}

export function isAuthPagePath(pathname: string): boolean {
	return ['/login', '/logout', '/oidc', '/auth/oidc', '/mobile/passkey'].some((prefix) => pathname.startsWith(prefix));
}

type UnauthorizedAction = 'none' | 'redirect' | 'reload' | 'retry';

function reloadDocumentOnce() {
	if (reloadForNewBuildStarted) return;
	reloadForNewBuildStarted = true;
	window.location.reload();
}

async function reloadForNewBuild() {
	await tokenRefreshHandler?.();
	reloadDocumentOnce();
}

export async function handleUnauthorizedResponse(
	requestPath: string,
	retry = false,
	serverMsg?: string | null
): Promise<UnauthorizedAction> {
	const session = sessionController.signal;
	if (typeof window === 'undefined' || retry || session.aborted) {
		return 'none';
	}

	const isVersionMismatch = serverMsg?.toLowerCase().includes('application has been updated') ?? false;
	const isAuthApi = skipAuthPaths.some((path) => requestPath.startsWith(path));
	const pathname = window.location.pathname || '/';

	if (isAuthApi || isAuthPagePath(pathname)) {
		return 'none';
	}
	if (unauthorizedRedirectStarted) return 'redirect';

	// During a self-update, refresh so the new backend rotates tokens, then reload the stale document once.
	// Only transport failures are recoverable; a rejected refresh is terminal.
	const recoverable = isVersionMismatch || upgradeInProgress;

	if (!tokenRefreshHandler) return 'none';

	const needsReload = isVersionMismatch && upgradeInProgress;
	const recovery = await tryCatch<void | string | null>(needsReload ? reloadForNewBuild() : tokenRefreshHandler());
	if (session.aborted) return 'none';
	if (recovery.error === null) return needsReload ? 'reload' : 'retry';

	const error = recovery.error;
	const isTransientRefreshFailure = error instanceof APIError && (error.name === 'NetworkError' || error.name === 'TimeoutError');
	if (recoverable && isTransientRefreshFailure) return 'none';
	if (!unauthorizedRedirectStarted) {
		unauthorizedRedirectStarted = true;
		window.location.replace(`/login?redirect=${encodeURIComponent(pathname)}`);
	}
	return 'redirect';
}

async function probeDownload(url: string, retry = false): Promise<boolean> {
	const probe = new AbortController();
	const response = await fetch(url, { credentials: 'include', signal: withSessionSignal(probe.signal) });
	if (response.ok) {
		probe.abort();
		return true;
	}
	const body = await tryCatch(response.json());
	const message = extractServerMessage(body.data, true);
	if (response.status === 401 && !retry) {
		const path = new URL(url, window.location.origin).pathname;
		const action = await handleUnauthorizedResponse(path, false, message);
		if (action === 'retry') return probeDownload(url, true);
		if (action !== 'none') return false;
	}
	throw new Error(message ?? `${response.status} ${response.statusText}`.trim());
}

class APIClient {
	defaults: { baseURL: string };

	constructor(baseURL: string) {
		this.defaults = { baseURL: baseURL.endsWith('/') ? baseURL.slice(0, -1) : baseURL };
	}

	private async performRequest<T = unknown>(
		method: string,
		url: string,
		data?: unknown,
		config: InternalRequestConfig = {}
	): Promise<APIResponse<T>> {
		const baseURL = config.baseURL ?? this.defaults.baseURL;
		let requestUrl = url;
		if (!URL.canParse(url)) {
			requestUrl = url.startsWith('/') ? `${baseURL}${url}` : `${baseURL}/${url}`;
		}
		const requestConfig = { ...config, baseURL, method, url };
		let requestPath = requestUrl;
		try {
			requestPath = new URL(requestUrl, 'http://localhost').pathname;
		} catch {
			// Fall back to the raw request URL.
		}
		if (requestPath.startsWith('/api')) requestPath = requestPath.slice(4) || '/';

		// Requests needed to sign in again must keep working after logout.
		const isSessionFree =
			!requestPath.startsWith('/auth/refresh') &&
			(skipAuthPaths.some((prefix) => requestPath.startsWith(prefix)) ||
				['/oidc/url', '/oidc/callback', '/oidc/status'].includes(requestPath) ||
				requestPath.startsWith('/app/version') ||
				requestPath.endsWith('/settings/public'));
		const session = isSessionFree ? undefined : sessionController.signal;
		const deadline = new AbortController();
		const signals = [session ? withSessionSignal(config.signal) : config.signal, deadline.signal].filter(
			(entry) => entry !== undefined
		);
		const upperMethod = method.toUpperCase();

		async function sendRequest(): Promise<APIResponse<T>> {
			const headers = new Headers(config.headers);
			if (activeActivityBatchId && !['GET', 'HEAD'].includes(upperMethod) && !headers.has('X-Arcane-Batch-Id')) {
				headers.set('X-Arcane-Batch-Id', activeActivityBatchId);
			}
			const init: RequestInit = {
				cache: config.cache,
				credentials: 'include',
				method: upperMethod,
				headers,
				signal: AbortSignal.any(signals)
			};
			const body = data ?? config.data;
			if (
				body instanceof FormData ||
				body instanceof URLSearchParams ||
				body instanceof Blob ||
				body instanceof ArrayBuffer ||
				ArrayBuffer.isView(body) ||
				typeof body === 'string'
			) {
				init.body = body as BodyInit;
			} else if (body !== undefined && body !== null) {
				init.body = JSON.stringify(body);
				if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
			}

			let headersTimer: ReturnType<typeof setTimeout> | undefined;
			if (config.timeout) {
				headersTimer = setTimeout(() => deadline.abort(new DOMException('Request timed out', 'TimeoutError')), config.timeout);
			}
			let response: Response;
			try {
				response = await fetch(withSearchParams(requestUrl, config.params), init);
			} finally {
				clearTimeout(headersTimer);
			}
			if (!response.ok) throw new HTTPStatusError(response);
			const parsed = upperMethod === 'HEAD' ? undefined : await parseResponseBody(response.clone(), config.responseType);
			if (session?.aborted) throw new SessionCancelledError();
			const parsedRecord = asRecord(parsed);
			const failedEnvironmentsHeader = response.headers.get('X-Arcane-Failed-Environments');
			if (failedEnvironmentsHeader) config.onFailedEnvironments?.(JSON.parse(decodeURIComponent(failedEnvironmentsHeader)));
			if (Array.isArray(parsedRecord?.['failedEnvironments'])) config.onFailedEnvironments?.(parsedRecord['failedEnvironments']);
			if (Array.isArray(parsedRecord?.['environments'])) config.onEnvironments?.(parsedRecord['environments']);
			return {
				data: parsed as T,
				headers: response.headers,
				raw: response,
				status: response.status
			};
		}

		const sent = await tryCatch(sendRequest());
		if (sent.error === null) return sent.data;

		const error = sent.error;
		if (session?.aborted) throw new SessionCancelledError();
		if (!(error instanceof HTTPStatusError)) {
			let message = 'Unknown error';
			if (error instanceof Error) message = error.name === 'TimeoutError' ? 'Request timed out' : error.message;
			let name = error instanceof Error ? error.name : undefined;
			if (error instanceof TypeError) name = 'NetworkError';
			throw new APIError(message, {
				cause: error,
				config: requestConfig,
				name: name || undefined,
				requestUrl
			});
		}

		const errorResponse = error.response;
		let parsed: unknown;
		if (Number(errorResponse.headers.get('Content-Length') ?? 0) <= maxErrorBodyBytes) {
			let errorBodyTimer: ReturnType<typeof setTimeout> | undefined;
			const errorBody = await tryCatch(
				Promise.race([
					parseResponseBody(errorResponse.clone()),
					new Promise<undefined>((resolve) => {
						errorBodyTimer = setTimeout(() => resolve(undefined), errorBodyTimeoutMs);
					})
				])
			);
			clearTimeout(errorBodyTimer);
			parsed = errorBody.data ?? undefined;
		}
		deadline.abort();
		if (session?.aborted) throw new SessionCancelledError();

		if (errorResponse.status === 401) {
			const action = await handleUnauthorizedResponse(requestPath, config._retry, extractServerMessage(parsed));
			if (session?.aborted) throw new SessionCancelledError();
			if (action === 'retry') {
				return this.performRequest<T>(method, url, data, { ...config, _retry: true });
			}
			if (action === 'redirect' || action === 'reload') {
				return new Promise(() => {});
			}
		}

		if (errorResponse.status === 403 && typeof window !== 'undefined' && !config.suppressAccessDeniedToast) {
			const reason = extractServerMessage(parsed) ?? 'You do not have permission to perform this action.';
			toast.error(m.common_access_denied(), { description: reason });
		}

		throw new APIError(extractServerMessage(parsed, true) ?? error.message, {
			cause: error,
			config: requestConfig,
			name: 'HTTPError',
			requestUrl,
			response: { data: parsed, headers: errorResponse.headers, raw: errorResponse, status: errorResponse.status }
		});
	}

	get<T = unknown>(url: string, config?: APIRequestConfig) {
		return this.performRequest<T>('GET', url, undefined, config);
	}

	post<T = unknown>(url: string, data?: unknown, config?: APIRequestConfig) {
		return this.performRequest<T>('POST', url, data, config);
	}

	put<T = unknown>(url: string, data?: unknown, config?: APIRequestConfig) {
		return this.performRequest<T>('PUT', url, data, config);
	}

	patch<T = unknown>(url: string, data?: unknown, config?: APIRequestConfig) {
		return this.performRequest<T>('PATCH', url, data, config);
	}

	delete<T = unknown>(url: string, config?: APIRequestConfig) {
		return this.performRequest<T>('DELETE', url, undefined, config);
	}

	head<T = unknown>(url: string, config?: APIRequestConfig) {
		return this.performRequest<T>('HEAD', url, undefined, config);
	}
}

const devBackendUrl = typeof process !== 'undefined' ? process?.env?.['DEV_BACKEND_URL'] : undefined;
export const apiClient = new APIClient(devBackendUrl || '/api');

abstract class BaseAPIService {
	api = apiClient;

	static setTokenRefreshHandler(handler: () => Promise<string | null>) {
		tokenRefreshHandler = handler;
	}

	// Toggled by the update flows (Update All / local update center) so an in-progress
	// self-update restart is treated as a recoverable reconnect, not a logout.
	static setUpgradeInProgress(value: boolean) {
		upgradeInProgress = value;
	}

	static async reloadForNewBuild() {
		const result = await tryCatch(reloadForNewBuild());
		if (result.error !== null) reloadDocumentOnce();
	}

	/** Cancels the current session's work and blocks protected requests until beginSession. */
	static endSession() {
		sessionController.abort(new SessionCancelledError());
	}

	static beginSession() {
		if (sessionController.signal.aborted) {
			sessionController = new AbortController();
		}
	}

	protected async downloadFile(path: string, params?: Record<string, string>): Promise<void> {
		const query = new URLSearchParams(params).toString();
		const url = `${this.api.defaults.baseURL}${path}${query ? `?${query}` : ''}`;
		if (await probeDownload(url)) {
			downloadFromUrl(url);
		}
	}

	protected async handleResponse<T>(promise: Promise<APIResponse>): Promise<T> {
		const payload = (await promise).data;
		const inner = asRecord(payload)?.['data'];
		return (inner !== undefined ? inner : payload) as T;
	}
}

export default BaseAPIService;
