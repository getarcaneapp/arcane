import type { Locale } from '$lib/paraglide/runtime';
import type { RoleAssignmentSummary } from '$lib/types/role.type';

export type User = {
	id: string;
	username: string;
	passwordHash?: string;
	displayName?: string;
	email?: string;
	/** Role assignments held by this user. */
	roleAssignments: RoleAssignmentSummary[];
	/**
	 * Server-resolved effective permissions, keyed by environment ID. The
	 * 'global' key holds permissions that apply across every environment AND
	 * to org-level endpoints. The value `['*']` under 'global' is a sentinel
	 * meaning "every permission" (sudo callers).
	 */
	permissionsByEnv: Record<string, string[]>;
	canDelete?: boolean;
	createdAt: string;
	lastLogin?: string;
	updatedAt?: string;
	oidcSubjectId?: string;
	locale?: Locale;
	requiresPasswordChange?: boolean;
};

export type CreateUser = Omit<
	User,
	| 'id'
	| 'createdAt'
	| 'updatedAt'
	| 'lastLogin'
	| 'oidcSubjectId'
	| 'passwordHash'
	| 'requiresPasswordChange'
	| 'roleAssignments'
	| 'permissionsByEnv'
> & {
	password: string;
};
