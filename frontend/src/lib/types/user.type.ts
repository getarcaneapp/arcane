import type { Locale } from '$lib/paraglide/runtime';

export type User = {
	id: string;
	username: string;
	passwordHash?: string;
	displayName?: string;
	email?: string;
	roles: string[];
	createdAt: string;
	lastLogin?: string;
	updatedAt?: string;
	oidcSubjectId?: string;
	locale?: Locale;
	requiresPasswordChange?: boolean;
	mobileDockTabs?: string[];
};

export type CreateUser = Omit<
	User,
	'id' | 'createdAt' | 'updatedAt' | 'lastLogin' | 'oidcSubjectId' | 'passwordHash' | 'requiresPasswordChange' | 'roles'
> & {
	password: string;
	roles?: string[];
};
