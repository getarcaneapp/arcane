import type { User } from '$lib/types/user.type';

export interface OidcUserInfo {
	sub: string;
	email: string;
	name?: string;
	displayName?: string;
	preferred_username?: string;
	given_name?: string;
	family_name?: string;
	picture?: string;
	groups?: string[];
}

export interface LoginCredentials {
	username: string;
	password: string;
}

export type LoginResponseData = {
	token: string;
	refreshToken: string;
	expiresAt: string;
	user: User;
	requirePasswordChange?: boolean;
};

/**
 * Auto-login configuration returned by the backend.
 * Password is never included in this response.
 */
export interface AutoLoginConfig {
	enabled: boolean;
	username: string;
}
