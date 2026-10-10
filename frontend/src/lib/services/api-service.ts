import ky, { HTTPError as KyHTTPError, TimeoutError, type Options as KyOptions, type SearchParamsOption } from 'ky';
import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export interface APIRequestConfig {
	baseURL?: string;
	cache?: RequestCache;
	data?: unknown;
	headers?: HeadersInit;
	params?: SearchParamsOption;
	responseType?: 'json' | 'text' | 'blob' | 'arrayBuffer';
	retry?: number;
	signal?: AbortSignal;
	suppressAccessDeniedToast?: boolean;
	timeout?: number | false;
}

interface InternalRequestConfig extends APIRequestConfig {
	_retry?: boolean;
}

export interface APIResponse<T = any> {
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

const problemMessageFactories: Record<string, () => string> = {
	'urn:arcane:problem:password-policy:basic': m.security_password_policy_basic_tooltip,
	'urn:arcane:problem:password-policy:standard': m.security_password_policy_standard_tooltip,
	'urn:arcane:problem:password-policy:strong': m.security_password_policy_strong_tooltip
};

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

	const problemType = nonEmptyString(inner['type']);
	const localizedMessage = problemType ? problemMessageFactories[problemType] : undefined;
	if (localizedMessage) return localizedMessage();

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

async function parseResponseBody(response: Response, responseType: APIRequestConfig['responseType'] = 'json'): Promise<any> {
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
// True while a manager self-update or fleet "Update All" runs; its version-mismatch 401s
// are recoverable and must not bounce the user to /login.
let upgradeInProgress = false;
// A confirmed manager restart still needs document recovery after polling ends.
let upgradeReloadPending = false;
let upgradeReloadStarted = false;
let unauthorizedRedirectStarted = false;
const skipAuthPaths = [
	'/auth/login',
	'/auth/logout',
	'/auth/refresh',
	'/auth/oidc',
	'/auth/passkey',
	'/auth/mfa',
	'/auth/auto-login',
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

export async function handleUnauthorizedResponseInternal(
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

	const operationResult = await tryCatch(
		(async () => {
			await tokenRefreshHandler();
			if (!isVersionMismatch || (!upgradeInProgress && !upgradeReloadPending)) return 'retry';
			if (!upgradeReloadStarted) {
				upgradeReloadStarted = true;
				window.location.reload();
			}
			return 'reload';
		})()
	);
	if (session.aborted) return 'none';
	if (operationResult.error === null) return operationResult.data;

	const error = operationResult.error;
	const isTransientRefreshFailure = error instanceof APIError && (error.name === 'NetworkError' || error.name === 'TimeoutError');
	if (recoverable && isTransientRefreshFailure) return 'none';
	if (!unauthorizedRedirectStarted) {
		unauthorizedRedirectStarted = true;
		window.location.replace(`/login?redirect=${encodeURIComponent(pathname)}`);
	}
	return 'redirect';
}

class APIClient {
	defaults: { baseURL: string };
	private client;

	constructor(baseURL: string) {
		this.defaults = { baseURL };
		this.client = ky.create({
			credentials: 'include',
			retry: 0,
			timeout: false
		});
	}

	setBaseURL(baseURL: string) {
		this.defaults.baseURL = baseURL;
	}

	private async performRequest<T = any>(
		method: string,
		url: string,
		data?: unknown,
		config: InternalRequestConfig = {}
	): Promise<APIResponse<T>> {
		const baseURL = config.baseURL ?? this.defaults.baseURL;
		const requestUrl = /^[a-z]+:\/\//i.test(url) ? url : `${baseURL.replace(/\/+$/, '')}/${url.replace(/^\/+/, '')}`;
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
				requestPath.startsWith('/app-version') ||
				requestPath.endsWith('/settings/public'));
		const session = isSessionFree ? undefined : sessionController.signal;
		const signal = session ? withSessionSignal(config.signal) : config.signal;
		const upperMethod = method.toUpperCase();

		const operationResult = await tryCatch(
			(async () => {
				const headers = new Headers(config.headers);
				if (activeActivityBatchId && !['GET', 'HEAD'].includes(upperMethod) && !headers.has('X-Arcane-Batch-Id')) {
					headers.set('X-Arcane-Batch-Id', activeActivityBatchId);
				}
				const options: KyOptions = {
					cache: config.cache,
					method,
					headers,
					retry: config.retry ?? 0,
					searchParams: config.params,
					signal,
					timeout: config.timeout ?? false
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
					options.body = body as BodyInit;
				} else if (body !== undefined && body !== null) {
					options.json = body;
				}

				const response = await this.client(requestUrl, options);
				const parsed = upperMethod === 'HEAD' ? undefined : await parseResponseBody(response.clone(), config.responseType);
				if (session?.aborted) throw new SessionCancelledError();
				return {
					data: parsed as T,
					headers: response.headers,
					raw: response,
					status: response.status
				};
			})()
		);
		if (operationResult.error === null) return operationResult.data;

		const error = operationResult.error;
		if (session?.aborted) throw new SessionCancelledError();
		if (!(error instanceof KyHTTPError)) {
			let message = 'Unknown error';
			if (error instanceof TimeoutError) message = 'Request timed out';
			else if (error instanceof Error) message = error.message;
			throw new APIError(message, {
				cause: error,
				config: requestConfig,
				name: (error instanceof Error && error.name) || undefined,
				requestUrl
			});
		}

		const errorResponse = error.response;
		const parsed =
			error.data !== undefined || errorResponse.bodyUsed ? error.data : await parseResponseBody(errorResponse.clone());
		if (session?.aborted) throw new SessionCancelledError();

		if (errorResponse.status === 401) {
			const action = await handleUnauthorizedResponseInternal(requestPath, config._retry, extractServerMessage(parsed));
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

	get<T = any>(url: string, config?: APIRequestConfig) {
		return this.performRequest<T>('GET', url, undefined, config);
	}

	post<T = any>(url: string, data?: unknown, config?: APIRequestConfig) {
		return this.performRequest<T>('POST', url, data, config);
	}

	put<T = any>(url: string, data?: unknown, config?: APIRequestConfig) {
		return this.performRequest<T>('PUT', url, data, config);
	}

	patch<T = any>(url: string, data?: unknown, config?: APIRequestConfig) {
		return this.performRequest<T>('PATCH', url, data, config);
	}

	delete<T = any>(url: string, config?: APIRequestConfig) {
		return this.performRequest<T>('DELETE', url, undefined, config);
	}

	head<T = any>(url: string, config?: APIRequestConfig) {
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
		if (!upgradeReloadPending && (!value || !upgradeInProgress)) {
			upgradeReloadStarted = false;
		}
		upgradeInProgress = value;
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

	static confirmUpgradeRestart() {
		upgradeReloadPending = true;
	}

	protected async handleResponse<T>(promise: Promise<APIResponse>): Promise<T> {
		const payload = (await promise).data;
		const inner = asRecord(payload)?.['data'];
		return (inner !== undefined ? inner : payload) as T;
	}
}

export default BaseAPIService;
