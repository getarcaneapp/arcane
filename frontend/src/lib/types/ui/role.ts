import type { OidcRoleMapping, Role } from '../role';
import type { CreateRequest, UpdateRequest } from './pagination';

export type RoleWritable = 'name' | 'description' | 'permissions';

export type RoleCreate = CreateRequest<Role, RoleWritable, 'name' | 'permissions'>;

export type RoleUpdate = UpdateRequest<Role, RoleWritable>;

export type OidcRoleMappingWritable = 'claimValue' | 'roleId' | 'environmentId';

export type OidcRoleMappingCreate = CreateRequest<OidcRoleMapping, OidcRoleMappingWritable, 'claimValue' | 'roleId'>;

export type OidcRoleMappingUpdate = UpdateRequest<OidcRoleMapping, OidcRoleMappingWritable>;
