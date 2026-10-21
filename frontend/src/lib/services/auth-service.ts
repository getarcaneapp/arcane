import { goto, refreshAll } from '$app/navigation';
import type { QueryClient } from '@tanstack/svelte-query';
import { Temporal } from 'temporal-polyfill';

import { activityStore } from '#lib/stores/activity.store.svelte.js';
import { dashboardStore } from '#lib/stores/dashboard.store.svelte.js';
import { environmentStatusStore } from '#lib/stores/environment-status.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';
import type { User, MFAChallenge, AuthenticationResponse } from '#lib/types/auth.js';
import type { LoginCredentials, AutoLoginConfig } from '#lib/types/auth.js';
import type { OidcStatusInfo } from '#lib/types/settings.js';
import { parseInstant } from '#lib/utils/formatting.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, { APIError, SessionCancelledError, currentSessionSignal } from './api-service';

const LEGACY_REFRESH_TOKEN_KEY = 'arcane_refresh_token';
const TOKEN_EXPIRY_KEY = 'arcane_token_expiry';
const REFRESH_LOCK_NAME = 'arcane-token-refresh';
const REFRESH_BUFFER_MS = 5 * 60 * 1000; // Refresh 5 minutes before expiry
const REFRESH_JITTER_MS = 5000;
const REFRESH_RACE_GRACE_MS = 3000;
const REFRESH_RACE_POLL_MS = 150;

export class MFARequiredError extends Error {
	constructor(public readonly challenge: MFAChallenge) {
		super('Multi-factor authentication is required');
		this.name = 'MFARequiredError';
	}
}

class AuthService extends BaseAPIService {
	private refreshTimer: ReturnType<typeof setTimeout> | null = null;
	private isRefreshing = false;
	private refreshGeneration = 0;
	private refreshSubscribers: Array<(token: string | null, error?: Error) => void> = [];
	private logoutRequest: Promise<boolean> | null = null;

	constructor() {
		super();
		BaseAPIService.setTokenRefreshHandler(() => this.refreshAccessToken());
		if (typeof window === 'undefined') return;
		try {
			sessionStorage.removeItem(LEGACY_REFRESH_TOKEN_KEY);
			sessionStorage.removeItem(TOKEN_EXPIRY_KEY);
		} catch (e) {
			console.error('Failed to remove legacy token data:', e);
		}
		const storedExpiry = this.getStoredExpiry();
		if (storedExpiry) this.scheduleTokenRefresh(storedExpiry);
		window.addEventListener('storage', (event) => {
			if (event.key !== TOKEN_EXPIRY_KEY) return;
			if (event.newValue) {
				this.scheduleTokenRefresh(event.newValue);
			} else {
				this.clearRefreshTimer();
			}
		});
	}

	private storeTokenData(expiresAt: string): void {
		if (typeof window === 'undefined') return;
		try {
			localStorage.setItem(TOKEN_EXPIRY_KEY, expiresAt);
			this.scheduleTokenRefresh(expiresAt);
		} catch (e) {
			console.error('Failed to store token data:', e);
		}
	}

	private getStoredExpiry(): string | null {
		if (typeof window === 'undefined') return null;
		try {
			return localStorage.getItem(TOKEN_EXPIRY_KEY);
		} catch {
			return null;
		}
	}

	private clearRefreshTimer(): void {
		if (this.refreshTimer) {
			clearTimeout(this.refreshTimer);
			this.refreshTimer = null;
		}
	}

	private clearTokenData(): void {
		if (typeof window === 'undefined') return;
		try {
			localStorage.removeItem(TOKEN_EXPIRY_KEY);
			this.clearRefreshTimer();
		} catch (e) {
			console.error('Failed to clear token data:', e);
		}
	}

	private scheduleTokenRefresh(expiresAt: string): void {
		if (typeof window === 'undefined') return;

		this.clearRefreshTimer();

		const expiryTime = parseInstant(expiresAt);
		if (!expiryTime) return;
		const timeUntilExpiry = Temporal.Now.instant().until(expiryTime).total('milliseconds');
		const refreshTime = Math.max(0, timeUntilExpiry - REFRESH_BUFFER_MS - Math.random() * REFRESH_JITTER_MS);

		if (refreshTime > 0) {
			this.refreshTimer = setTimeout(() => {
				tryCatch(this.refreshAccessToken()).then((result) => {
					if (result.error !== null) {
						const err = result.error;

						console.error('Background token refresh failed:', err);
					} else {
						return result.data;
					}
				});
			}, refreshTime);
		}
	}

	private refreshedElsewhere(expiryBefore: string | null): boolean {
		const current = this.getStoredExpiry();
		const currentInstant = current ? parseInstant(current) : null;
		if (!current || current === expiryBefore || !currentInstant) return false;
		const beforeInstant = expiryBefore ? parseInstant(expiryBefore) : null;
		if (beforeInstant && Temporal.Instant.compare(currentInstant, beforeInstant) <= 0) return false;
		return Temporal.Now.instant().until(currentInstant).total('milliseconds') > REFRESH_BUFFER_MS;
	}

	private async refreshedElsewhereSoon(expiryBefore: string | null): Promise<boolean> {
		const deadline = Date.now() + REFRESH_RACE_GRACE_MS;
		while (!this.refreshedElsewhere(expiryBefore)) {
			if (Date.now() >= deadline) return false;
			await new Promise((resolve) => setTimeout(resolve, REFRESH_RACE_POLL_MS));
		}
		return true;
	}

	private async requestTokenRefresh(expiryBefore: string | null, session: AbortSignal): Promise<string | null> {
		if (this.refreshedElsewhere(expiryBefore)) return null;

		const response = await this.handleResponse<{
			token?: string;
			expiresAt?: string;
		}>(this.api.post('/auth/refresh'));

		if (session.aborted) throw new SessionCancelledError();
		if (response.expiresAt) {
			this.storeTokenData(response.expiresAt);
		}
		return response.token || null;
	}

	async refreshAccessToken(): Promise<string | null> {
		if (this.isRefreshing) {
			return new Promise<string | null>((resolve, reject) => {
				this.refreshSubscribers.push((token: string | null, error?: Error) => {
					if (error) reject(error);
					else resolve(token);
				});
			});
		}

		this.isRefreshing = true;
		const generation = ++this.refreshGeneration;
		const session = currentSessionSignal();
		const expiryBefore = this.getStoredExpiry();

		const coordinated = typeof navigator !== 'undefined' && 'locks' in navigator;
		const operationResult = await tryCatch(
			coordinated
				? navigator.locks.request(REFRESH_LOCK_NAME, () => this.requestTokenRefresh(expiryBefore, session))
				: this.requestTokenRefresh(expiryBefore, session)
		);

		const subscribers = generation === this.refreshGeneration ? this.refreshSubscribers : [];
		if (generation === this.refreshGeneration) {
			this.refreshSubscribers = [];
			this.isRefreshing = false;
		}

		if (session.aborted) {
			const cancelled = new SessionCancelledError();
			subscribers.forEach((callback) => callback(null, cancelled));
			throw cancelled;
		}
		if (operationResult.error === null) {
			subscribers.forEach((callback) => callback(operationResult.data));
			return operationResult.data;
		}

		const error = operationResult.error;
		const rejected = error instanceof APIError && error.status === 401;
		if (rejected && (coordinated ? this.refreshedElsewhere(expiryBefore) : await this.refreshedElsewhereSoon(expiryBefore))) {
			subscribers.forEach((callback) => callback(null));
			return null;
		}
		const err = error instanceof Error ? error : new Error('Token refresh failed');
		console.error('Token refresh failed:', err);
		if (rejected) {
			this.clearTokenData();
		}
		subscribers.forEach((callback) => callback(null, err));
		throw err;
	}

	async login(credentials: LoginCredentials): Promise<User> {
		const data = await this.handleResponse<AuthenticationResponse>(this.api.post('/auth/login', credentials));
		if (data.status === 'mfa_required') {
			if (!data.mfa) {
				throw new Error('The server did not provide an MFA challenge');
			}
			throw new MFARequiredError(data.mfa);
		}

		const user = await this.completeAuthentication(data);
		await refreshAll();
		goto(getEffectiveLandingPage());

		return user;
	}

	async completeAuthentication(data: AuthenticationResponse): Promise<User> {
		if (data.status !== 'authenticated' || !data.user) {
			throw new Error('Authentication did not complete');
		}

		BaseAPIService.beginSession();
		if (data.expiresAt) {
			this.storeTokenData(data.expiresAt);
		}

		await userStore.setUser(data.user);
		return data.user;
	}

	async getCurrentUser(): Promise<User | null> {
		const operationResult = await tryCatch(
			(async () => {
				const response = await this.api.get('/auth/me');
				const user = (response.data.user as User) || (response.data.data as User);

				userStore.setUser(user);

				return user;
			})()
		);
		if (operationResult.error !== null) {
			userStore.clearUser();
			return null;
		} else {
			return operationResult.data;
		}
	}

	async getAuthUrl(redirectUri: string): Promise<string> {
		const response = (await this.handleResponse(this.api.post('/oidc/url', { redirectUri }))) as {
			authUrl: string;
		};
		return response.authUrl;
	}

	async handleCallback(code: string, state: string): Promise<AuthenticationResponse> {
		return this.handleResponse<AuthenticationResponse>(this.api.post('/oidc/callback', { code, state }));
	}

	async getStatus(): Promise<OidcStatusInfo> {
		return this.handleResponse(this.api.get('/oidc/status'));
	}

	async changePassword(currentPassword: string, newPassword: string): Promise<void> {
		await this.handleResponse(
			this.api.post('/auth/password', {
				currentPassword,
				newPassword
			})
		);
	}

	resetAuthenticatedState(queryClient: QueryClient, options?: { restartMountedStores?: boolean }): void {
		queryClient.clear();
		featureStore.clear();
		const restartActivityStore = activityStore.stop({ resetState: true });
		const restartDashboardStore = dashboardStore.stop({ resetState: true });
		const restartEnvironmentStatusStore = environmentStatusStore.stop();
		const restartVersionStore = versionStore.stop();
		userStore.clearUser();

		if (options?.restartMountedStores) {
			if (restartActivityStore) void activityStore.start();
			if (restartDashboardStore) void dashboardStore.start();
			if (restartEnvironmentStatusStore) void environmentStatusStore.start();
			if (restartVersionStore) versionStore.start();
		}
	}

	/** Ends the local session, then revokes the server session. Resolves false when revocation is unconfirmed. */
	logout(queryClient: QueryClient): Promise<boolean> {
		// Reuse the pending request only while it still belongs to the current session.
		if (this.logoutRequest && currentSessionSignal().aborted) return this.logoutRequest;

		BaseAPIService.endSession();
		this.clearTokenData();
		this.refreshSubscribers.forEach((callback) => callback(null, new SessionCancelledError()));
		this.refreshSubscribers = [];
		this.isRefreshing = false;
		this.refreshGeneration++;
		this.resetAuthenticatedState(queryClient);

		const request: Promise<boolean> = tryCatch(this.api.post('/auth/logout', undefined, { timeout: 10_000 })).then((result) => {
			if (this.logoutRequest === request) this.logoutRequest = null;
			return result.error === null;
		});
		this.logoutRequest = request;
		return request;
	}

	/**
	 * Get auto-login configuration from the backend.
	 * @returns The auto-login config, or null if the request fails
	 */
	async getAutoLoginConfig(): Promise<AutoLoginConfig | null> {
		const result = await tryCatch(this.handleResponse<AutoLoginConfig>(this.api.get('/auth/autologin')));
		// Silently fail - auto-login is optional.
		return result.data;
	}

	/**
	 * Attempt auto-login using server-configured credentials.
	 * The server handles the credentials - they are never exposed to the frontend.
	 * @returns The user if login succeeded, null otherwise
	 */
	async attemptAutoLogin(): Promise<User | null> {
		const result = await tryCatch(this.handleResponse<AuthenticationResponse>(this.api.post('/auth/autologin')));
		// Silently fail - fall back to normal login flow.
		if (result.error !== null || result.data.status === 'mfa_required') return null;
		return this.completeAuthentication(result.data);
	}
}

export const authService = new AuthService();
