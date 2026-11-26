import type { UserRoleAssignmentInput } from './role';
import type { CreateRequest, UpdateRequest } from './ui/pagination';

export type GroupMemberSource = 'manual' | 'oidc' | 'ldap';

export type Group = {
	id: string;
	name: string;
	description?: string | null;
	memberCount: number;
	roleAssignmentCount: number;
	createdAt: string;
	updatedAt?: string;
};

export type GroupMember = {
	id: string;
	groupId: string;
	userId: string;
	username: string;
	displayName?: string | null;
	email?: string | null;
	source: GroupMemberSource;
	createdAt: string;
};

export type GroupRoleAssignment = {
	id: string;
	groupId: string;
	roleId: string;
	environmentId?: string;
	source: GroupMemberSource;
	createdAt: string;
};

export type SetGroupMembers = {
	userIds: string[];
};

export type SetGroupRoleAssignments = {
	assignments: UserRoleAssignmentInput[];
};

export type GroupWritable = 'name' | 'description';
export type GroupCreate = CreateRequest<Group, GroupWritable, 'name'>;
export type GroupUpdate = UpdateRequest<Group, GroupWritable>;
