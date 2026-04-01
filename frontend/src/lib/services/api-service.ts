import ky, { HTTPError as KyHTTPError, TimeoutError, type Options as KyOptions, type SearchParamsOption } from 'ky';
import { toast } from 'svelte-sonner';

export interface APIRequestConfig {
	baseURL?: string;
	data?: unknown;
	headers?: HeadersInit;
	params?: SearchParamsOption;
	responseType?: 'json' | 'text' | 'blob' | 'arrayBuffer';
	retry?: number;
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

export class APIError extends Error {
	config: InternalRequestConfig & { method?: string; url?: string };
	request?: { url: string };
	response?: {
		data: any;
		headers: Headers;
		raw: Response;
		status: number;
	};
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
		super(message);
		this.name = options.name ?? 'APIError';
		if (options.cause !== undefined) {
			(this as Error & { cause?: unknown }).cause = options.cause;
		}
		this.config = options.config;
		this.request = options.requestUrl ? { url: options.requestUrl } : undefined;
		this.response = options.response
			? {
					data: options.response.data,
					headers: options.response.headers,
					raw: options.response.raw,
					status: options.response.status
				}
			: undefined;
		this.status = options.response?.status;
	}
}

function extractServerMessage(data: any, includeErrors = false): string | undefined {
	const inner = (data && typeof data === 'object' ? ((data as any).data ?? data) : data) as any;
	if (typeof inner === 'string') {
		return inner;
	}
	if (inner) {
		const msg = inner['error'] || inner['message'] || inner['detail'] || inner['error_description'];
		if (msg) return msg;
		if (includeErrors && Array.isArray(inner['errors']) && inner['errors'].length) {
			return inner['errors'][0]?.message || inner['errors'][0];
		}
	}
	return undefined;
}

function isBodyInit(value: unknown): value is BodyInit {
	return (
		value instanceof FormData ||
		value instanceof URLSearchParams ||
		value instanceof Blob ||
		value instanceof ArrayBuffer ||
		ArrayBuffer.isView(value) ||
		typeof value === 'string'
	);
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

function normalizeUrl(baseURL: string, url: string): string {
	if (/^[a-z]+:\/\//i.test(url)) {
		return url;
	}

	const trimmedBase = baseURL.replace(/\/+$/, '');
	const trimmedUrl = url.replace(/^\/+/, '');

	if (/^[a-z]+:\/\//i.test(trimmedBase)) {
		return new URL(trimmedUrl, `${trimmedBase}/`).toString();
	}

	return `${trimmedBase}/${trimmedUrl}`;
}

function getRequestPath(url: string, baseURL: string): string {
	let reqUrl = url;
	try {
		if (/^https?:\/\//i.test(reqUrl)) {
			reqUrl = new URL(reqUrl).pathname;
		} else if (baseURL && /^https?:\/\//i.test(baseURL)) {
			reqUrl = new URL(reqUrl.replace(/^\/+/, ''), `${baseURL.replace(/\/+$/, '')}/`).pathname;
		}
	} catch {
		// ignore URL parse errors and fall back to the raw request URL
	}

	if (reqUrl.startsWith('/api')) {
		reqUrl = reqUrl.slice(4) || '/';
	}

	return reqUrl;
}

let tokenRefreshHandler: (() => Promise<string | null>) | null = null;

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
		const requestUrl = normalizeUrl(baseURL, url);
		const requestConfig = {
			...config,
			baseURL,
			method,
			url
		};

		try {
			const options: KyOptions = {
				method,
				headers: config.headers,
				retry: config.retry ?? 0,
				searchParams: config.params,
				timeout: config.timeout
			};

			const bodyData = data !== undefined ? data : config.data;
			if (bodyData !== undefined && bodyData !== null) {
				if (isBodyInit(bodyData)) {
					options.body = bodyData;
				} else {
					options.json = bodyData;
				}
			}

			const response = await this.client(requestUrl, options);
			const parsed = method.toUpperCase() === 'HEAD' ? undefined : await parseResponseBody(response.clone(), config.responseType);
			return {
				data: parsed as T,
				headers: response.headers,
				raw: response,
				status: response.status
			};
		} catch (error) {
			if (error instanceof KyHTTPError) {
				const errorResponse = error.response;
				const parsed = await parseResponseBody(errorResponse.clone(), config.responseType);
				const response: APIResponse = {
					data: parsed,
					headers: errorResponse.headers,
					raw: errorResponse,
					status: errorResponse.status
				};

				if (errorResponse.status === 401 && typeof window !== 'undefined' && !config._retry) {
					const serverMsg = extractServerMessage(parsed);
					const isVersionMismatch = serverMsg?.toLowerCase().includes('application has been updated');
					const reqUrl = getRequestPath(url, baseURL);
					const skipAuthPaths = [
						'/auth/login',
						'/auth/logout',
						'/auth/refresh',
						'/auth/oidc',
						'/auth/oidc/login',
						'/auth/oidc/callback',
						'/auth/auto-login',
						'/auth/auto-login-config',
						'/settings/public'
					];
					const isAuthApi = skipAuthPaths.some((path) => reqUrl.startsWith(path));
					const pathname = window.location.pathname || '/';
					const isOnAuthPage =
						pathname.startsWith('/login') ||
						pathname.startsWith('/logout') ||
						pathname.startsWith('/oidc') ||
						pathname.startsWith('/auth/oidc');

					if (!isAuthApi && !isOnAuthPage && tokenRefreshHandler) {
						try {
							await tokenRefreshHandler();
							return this.performRequest<T>(method, url, data, {
								...config,
								_retry: true
							});
						} catch {
							if (isVersionMismatch) {
								toast.info('Application has been updated. Please log in again.');
							}
							const redirectTo = encodeURIComponent(pathname);
							window.location.replace(`/login?redirect=${redirectTo}`);
							return new Promise(() => {});
						}
					}

					if (!isAuthApi && !isOnAuthPage && isVersionMismatch) {
						toast.info('Application has been updated. Please log in again.');
						const redirectTo = encodeURIComponent(pathname);
						window.location.replace(`/login?redirect=${redirectTo}`);
						return new Promise(() => {});
					}
				}

				throw new APIError(extractServerMessage(parsed, true) ?? error.message, {
					cause: error,
					config: requestConfig,
					name: 'HTTPError',
					requestUrl,
					response
				});
			}

			if (error instanceof TimeoutError) {
				throw new APIError('Request timed out', {
					cause: error,
					config: requestConfig,
					name: 'TimeoutError',
					requestUrl
				});
			}

			if (error instanceof Error) {
				throw new APIError(error.message, {
					cause: error,
					config: requestConfig,
					name: error.name || 'APIError',
					requestUrl
				});
			}

			throw new APIError('Unknown error', {
				cause: error,
				config: requestConfig,
				requestUrl
			});
		}
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

	protected async handleResponse<T>(promise: Promise<APIResponse>): Promise<T> {
		const response = await promise;
		const payload = response.data;
		const extracted =
			payload && typeof payload === 'object' && 'data' in payload && (payload as any).data !== undefined
				? (payload as any).data
				: payload;
		return extracted as T;
	}
}

export default BaseAPIService;
