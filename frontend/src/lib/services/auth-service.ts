import { goto, invalidateAll } from '$app/navigation';
import BaseAPIService from './api-service';
import userStore from '$lib/stores/user-store';
import type { User } from '$lib/types/user.type';
import type { OidcStatusInfo } from '$lib/types/settings.type';
import type { OidcUserInfo, LoginCredentials, LoginResponseData, AutoLoginConfig } from '$lib/types/auth.type';

const REFRESH_TOKEN_KEY = 'arcane_refresh_token';
const TOKEN_EXPIRY_KEY = 'arcane_token_expiry';
const REFRESH_BUFFER_MS = 5 * 60 * 1000; // Refresh 5 minutes before expiry

export class AuthService extends BaseAPIService {
	private refreshTimer: ReturnType<typeof setTimeout> | null = null;
	private isRefreshing = false;
	private refreshSubscribers: Array<(token: string | null, error?: Error) => void> = [];

	constructor() {
		super();
		BaseAPIService.setTokenRefreshHandler(() => this.refreshAccessToken());
	}

	private storeTokenData(refreshToken: string, expiresAt: string): void {
		if (typeof window === 'undefined') return;
		try {
			sessionStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
			sessionStorage.setItem(TOKEN_EXPIRY_KEY, expiresAt);
			this.scheduleTokenRefresh(expiresAt);
		} catch (e) {
			console.error('Failed to store token data:', e);
		}
	}

	private getStoredRefreshToken(): string | null {
		if (typeof window === 'undefined') return null;
		try {
			return sessionStorage.getItem(REFRESH_TOKEN_KEY);
		} catch {
			return null;
		}
	}

	private clearTokenData(): void {
		if (typeof window === 'undefined') return;
		try {
			sessionStorage.removeItem(REFRESH_TOKEN_KEY);
			sessionStorage.removeItem(TOKEN_EXPIRY_KEY);
			if (this.refreshTimer) {
				clearTimeout(this.refreshTimer);
				this.refreshTimer = null;
			}
		} catch (e) {
			console.error('Failed to clear token data:', e);
		}
	}

	private scheduleTokenRefresh(expiresAt: string): void {
		if (typeof window === 'undefined') return;

		if (this.refreshTimer) {
			clearTimeout(this.refreshTimer);
		}

		const expiryTime = new Date(expiresAt).getTime();
		const now = Date.now();
		const timeUntilExpiry = expiryTime - now;
		const refreshTime = Math.max(0, timeUntilExpiry - REFRESH_BUFFER_MS);

		if (refreshTime > 0) {
			this.refreshTimer = setTimeout(() => {
				this.refreshAccessToken().catch((err) => {
					console.error('Background token refresh failed:', err);
				});
			}, refreshTime);
		}
	}

	async refreshAccessToken(): Promise<string | null> {
		const refreshToken = this.getStoredRefreshToken();
		if (!refreshToken) {
			throw new Error('No refresh token available');
		}

		if (this.isRefreshing) {
			return new Promise<string | null>((resolve, reject) => {
				this.refreshSubscribers.push((token: string | null, error?: Error) => {
					if (error) reject(error);
					else resolve(token);
				});
			});
		}

		this.isRefreshing = true;

		try {
			const response = await this.handleResponse<{
				token?: string;
				refreshToken?: string;
				expiresAt?: string;
			}>(this.api.post('/auth/refresh', { refreshToken }));

			if (response.refreshToken && response.expiresAt) {
				this.storeTokenData(response.refreshToken, response.expiresAt);
			}

			const token = response.token || null;
			this.refreshSubscribers.forEach((callback) => callback(token));
			this.refreshSubscribers = [];
			this.isRefreshing = false;

			return token;
		} catch (error) {
			const err = error instanceof Error ? error : new Error('Token refresh failed');
			console.error('Token refresh failed:', err);
			this.clearTokenData();
			this.refreshSubscribers.forEach((callback) => callback(null, err));
			this.refreshSubscribers = [];
			this.isRefreshing = false;
			throw err;
		}
	}

	async login(credentials: LoginCredentials): Promise<User> {
		const data = await this.handleResponse<LoginResponseData>(this.api.post('/auth/login', credentials));
		const user = data.user as User;

		if (data.refreshToken && data.expiresAt) {
			this.storeTokenData(data.refreshToken, data.expiresAt);
		}

		userStore.setUser(user);
		await invalidateAll();
		goto('/dashboard');

		return user;
	}

	async getCurrentUser(): Promise<User | null> {
		try {
			const response = await this.api.get('/auth/me');
			const user = (response.data.user as User) || (response.data.data as User);

			userStore.setUser(user);

			return user;
		} catch {
			userStore.clearUser();
			return null;
		}
	}

	async getAuthUrl(redirectUri: string): Promise<string> {
		const response = (await this.handleResponse(this.api.post('/oidc/url', { redirectUri }))) as {
			authUrl: string;
		};
		return response.authUrl;
	}

	async handleCallback(
		code: string,
		state: string
	): Promise<{
		success: boolean;
		token?: string;
		refreshToken?: string;
		expiresAt?: string;
		user?: OidcUserInfo;
		error?: string;
	}> {
		const response = await this.handleResponse<{
			success: boolean;
			token?: string;
			refreshToken?: string;
			expiresAt?: string;
			user?: OidcUserInfo;
			error?: string;
		}>(this.api.post('/oidc/callback', { code, state }));

		if (response.refreshToken && response.expiresAt) {
			this.storeTokenData(response.refreshToken, response.expiresAt);
		}

		return response;
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

	logout(): void {
		this.clearTokenData();
		userStore.clearUser();
	}

	/**
	 * Get auto-login configuration from the backend.
	 * @returns The auto-login config, or null if the request fails
	 */
	async getAutoLoginConfig(): Promise<AutoLoginConfig | null> {
		try {
			const response = await this.handleResponse<AutoLoginConfig>(this.api.get('/auth/auto-login-config'));
			return response;
		} catch {
			// Silently fail - auto-login is optional
			return null;
		}
	}

	/**
	 * Attempt auto-login using server-configured credentials.
	 * The server handles the credentials - they are never exposed to the frontend.
	 * @returns The user if login succeeded, null otherwise
	 */
	async attemptAutoLogin(): Promise<User | null> {
		try {
			const data = await this.handleResponse<LoginResponseData>(this.api.post('/auth/auto-login'));
			const user = data.user as User;

			if (data.refreshToken && data.expiresAt) {
				this.storeTokenData(data.refreshToken, data.expiresAt);
			}

			await userStore.setUser(user);
			return user;
		} catch {
			// Silently fail - fall back to normal login flow
			return null;
		}
	}
}

export const authService = new AuthService();
