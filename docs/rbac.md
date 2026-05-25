# Role-Based Access Control

Arcane uses a role-based access control (RBAC) system to govern what users and API keys can do. Every action — starting a container, deploying a project, editing settings — is gated by a specific permission. Permissions are bundled into roles, and roles are assigned to users (optionally scoped to a specific environment).

If you're upgrading from an earlier version of Arcane, see the [Upgrade & Migration](#upgrade--migration) section below.

## The mental model

There are four moving parts:

- **Permissions** are fine-grained strings of the form `<resource>:<action>` — for example `containers:start`, `projects:deploy`, `users:create`. You don't pick these one-by-one; you bundle them into roles.
- **Roles** are named permission sets. Arcane ships with four built-in roles (Admin, Editor, Deployer, Viewer) and lets admins create custom ones.
- **Role assignments** bind a user to a role, optionally scoped to a single environment. A user can hold many assignments — for example *Admin globally*, or *Editor on production + Viewer on staging*.
- **OIDC mappings** translate SSO group claims into role assignments automatically on every login.

Permissions split into two scopes:

- **Org-level permissions** apply across the whole instance. Settings, users, registries, templates, environments management — these are all org-level. To get an org-level permission you need a **globally-scoped** role assignment.
- **Env-scoped permissions** apply to one environment at a time. Containers, projects, images, volumes, networks, swarm, GitOps — these are all env-scoped. You can hold a different role on each environment.

A user with `Editor on production` can manage Docker resources on that environment, but cannot change settings (org-level), and has no access at all on other environments.

## Built-in roles

The six built-in roles cover the common access patterns. They are immutable — you can't edit or delete them, but you can clone one as a starting point for a custom role.

| Role                 | Intended use                                                 | What it can do                                                                                                                                                                |
| -------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Admin**            | Full instance operators                                      | Everything. Manages users, roles, settings, registries, and every Docker resource on every environment.                                                                       |
| **Editor**           | Day-to-day Docker management                                 | Read+write on containers, projects, images, volumes, networks, swarm resources, GitOps syncs, webhooks, jobs, notifications, vulnerabilities. Read-only on settings/users.    |
| **No-Shell Editor**  | Editor without `exec` access                                 | Same as Editor, but cannot open an interactive shell inside a running container.                                                                                              |
| **Deployer**         | CI/CD service accounts and on-call responders                | Deploy projects, restart/start/stop/redeploy containers, sync GitOps, pull images. Cannot create or delete resources, cannot manage settings or users.                        |
| **Monitor**          | Observability and on-call read access                        | Read Docker resources, view container/project/service logs, dashboards, events. No mutations, no exec, no settings access.                                                    |
| **Viewer**           | Auditors and read-only stakeholders                          | Read-only access to every Docker resource and to most org-level pages. Cannot view logs, start, stop, change, or delete anything.                                             |

You must have at least one user holding **Admin** globally at all times. Arcane will refuse role-assignment edits, OIDC mapping changes, and user deletions that would leave the system with zero global admins.

## Custom roles

> **Admin-only:** creating, editing, and deleting roles — and assigning roles to users — is reserved for global admins. The same applies to OIDC group mappings. These actions are intentionally not delegatable as fine-grained permissions; the matching API routes return 403 to non-admins.

Admins can create custom roles to fit narrower job functions — for example, "Database Operator" that can manage only volumes and backups, or "Security Reviewer" that can read vulnerability scans but trigger nothing.

To create a custom role:

1. Open **Settings → Roles**.
2. Click **Create role**.
3. Name the role, optionally add a description.
4. In the permission picker on the right, expand each resource group and check the actions this role should grant. A "select all" checkbox at the top of each group is handy for full-read or full-write roles.
5. Save.

Custom roles can be edited and deleted at any time. Built-in roles show a badge and the editor is read-only — use the **Clone as custom role** button to start a new role pre-populated with a built-in's permissions.

When you delete a custom role, every assignment of it is also removed. The global-admin guard still applies — if removing the role would orphan the last global Admin, the deletion is refused.

## Assigning roles to users

From **Settings → Users**, open a user. The **Role assignments** section shows every role they hold, with a scope label (Global or a specific environment name). You can:

- Add a row to grant a new role on an environment (or globally).
- Remove a row to revoke that grant.
- Use the **Global** scope to grant org-level permissions.

A user with no assignments at all can authenticate but cannot read or do anything — they'll land on a "no access" screen until an admin grants them a role.

### OIDC users

If a user authenticated via OIDC matches an OIDC role mapping, their assignments come from the mapping table and are managed there. The user editor will be read-only for those assignments and link to **Settings → OIDC Mappings**. Manual assignments on OIDC users (for groups that have no mapping) still work and are preserved across logins.

## OIDC group mappings

For SSO-only deployments, role assignment can be driven entirely from your IdP. Map an OIDC group claim value to a role and an optional environment scope; on every login Arcane reads the user's group claim and re-syncs their `source=oidc` assignments to match.

### Setup

1. Confirm your IdP issues a groups claim. By default Arcane reads it from the `groups` claim — you can change this in **Settings → Settings** under **OIDC Groups Claim**. Common alternatives are `realm_access.roles` (Keycloak), `memberOf` (Azure AD), or a custom claim path.
2. Open **Settings → OIDC Mappings**.
3. Click **Add mapping** and fill in:
   - **Claim value**: the exact string that appears in the user's groups claim (e.g. `docker-admins`)
   - **Role**: the role to grant when this claim is present
   - **Environment scope**: `Global` for org-level role, or a specific environment
4. Save.

A user who is a member of multiple mapped groups receives the **union** of the matching assignments. A user demoted in the IdP loses their OIDC-sourced assignments on next login; their manual assignments (if any) remain.

The legacy `OidcAdminClaim` / `OidcAdminValue` settings continue to work as an implicit "matching claim grants Admin globally" mapping. New deployments should configure mappings explicitly and leave those settings empty.

### Declaring mappings via env (GitOps / IaC)

For deployments that prefer to keep everything in source control, mappings can be declared via the `OIDC_ROLE_MAPPINGS` env var. The value is a JSON array of `{claimValue, roleId, environmentId?}` entries; reconciliation runs at every boot and is fully declarative — adding an entry creates it, removing one deletes it, editing one updates it. Manual mappings created in the UI are never touched.

```
OIDC_ROLE_MAPPINGS='[
  {"claimValue": "docker-admins", "roleId": "role_admin"},
  {"claimValue": "devops",        "roleId": "role_editor", "environmentId": "env-prod"}
]'
```

Env-managed rows are flagged with a `ENV` badge in the UI and are read-only — the Edit/Delete actions are disabled, and the API returns 409 if you try to mutate them. To change one, edit the env var and restart. Set `OIDC_ROLE_MAPPINGS='[]'` to wipe every env-managed row. The matching `OIDC_ROLE_MAPPINGS_FILE` env var works for Docker secret files.

## API keys

Every API key now carries its own permission set, independent of the owning user. This means you can issue narrow-scoped keys for CI/CD ("can deploy projects on prod, nothing else") without granting the owning user the same scope.

### Creating a scoped API key

1. Open **Settings → API Keys** and click **Create API key**.
2. Fill in name, description, optional expiration.
3. In the **Permissions** section, check the permissions this key should hold. Use environment scope just like role assignments.
4. Save and copy the key value — it is shown only once.

You cannot grant a key permissions you don't have yourself. Server-side validation rejects creating a key whose permission set exceeds the creator's effective permission set.

### What happens to existing keys on upgrade

Existing API keys created before this release inherit a snapshot of their owner's current effective permissions at upgrade time, scoped per the key's environment binding. If an owner's permissions change later, their existing keys are *not* automatically re-scoped — admins should review and re-issue keys after large permission changes.

## The permission catalog

Permissions follow `<resource>:<action>`. You don't usually pick them individually — the role editor groups them by resource — but here's the catalog for reference.

### Org-level (require global-scope role)

- **users**: `list`, `read`, `create`, `update`, `delete`
- **roles**: `list`, `read` *(creating, editing, deleting, and assigning roles are admin-only — not delegatable)*
- **apikeys**: `list`, `read`, `create`, `update`, `delete`
- **settings**: `read`, `write`
- **environments**: `list`, `read`, `create`, `update`, `delete`, `pair`, `sync`
- **registries** (container registries): `list`, `read`, `create`, `update`, `delete`, `test`
- **templates**: `list`, `read`, `create`, `update`, `delete`
- **git-repositories**: `list`, `read`, `create`, `update`, `delete`, `test`, `sync`
- **events**: `read`
- **customize**: `manage`

### Env-scoped (per environment)

- **containers**: `list`, `read`, `logs`, `create`, `start`, `stop`, `restart`, `redeploy`, `delete`, `exec`, `autoupdate`
- **projects**: `list`, `read`, `logs`, `create`, `update`, `deploy`, `down`, `restart`, `delete`, `archive`
- **images**: `list`, `read`, `pull`, `push`, `build`, `prune`, `delete`, `upload`
- **volumes**: `list`, `read`, `create`, `delete`, `prune`, `browse`, `upload`, `backup`
- **networks**: `list`, `read`, `create`, `delete`, `prune`
- **swarm**: `read`, `init`, `join`, `leave`, `spec`, `nodes`, `services`, `services:logs`, `stacks`, `configs`, `secrets`, `unlock`
- **gitops**: `list`, `read`, `create`, `update`, `delete`, `sync`
- **webhooks**: `list`, `create`, `update`, `delete`
- **jobs**: `manage`
- **notifications**: `manage`
- **dashboard**: `read`
- **system**: `read`, `prune`, `upgrade`
- **image-updates**: `read`, `check`
- **vulnerabilities**: `read`, `scan`, `manage`
- **build-workspaces**: `manage`

## Upgrade & migration

When you upgrade from a pre-RBAC release of Arcane, the migration runs automatically the first time the new server starts:

- Every user that held the legacy `admin` role gets a **global Admin** assignment.
- Every other user gets a single **global Viewer** assignment (read-only across everything, including the Settings area).
- Existing API keys inherit a snapshot of their owner's effective permissions.

If for some reason the migration would leave no global admins, Arcane refuses to start and logs an error — restore from backup and investigate before retrying.

After the upgrade you should:

1. **Review your admins.** `Settings → Users` shows every user and their assignments at a glance.
2. **Right-size non-admins.** Defaulting everyone to global Viewer is safe but very restrictive (read-only, no logs). Promote individual users to Editor / No-Shell Editor / Deployer / Monitor on the environments they actually use.
3. **Map OIDC groups, if you use SSO.** Set the `OIDC Groups Claim` and add mappings under `Settings → OIDC Mappings`. Existing legacy `OidcAdminClaim` / `OidcAdminValue` settings keep working, but mappings give you finer control.
4. **Review API keys.** Each key now carries its own permissions; the upgrade gives them a snapshot of the owner's permissions, which is the most permissive safe default. Tighten down CI/CD keys to least privilege.

## Troubleshooting

**"permission denied: ..." errors after upgrade.** Find the permission string in the error, look it up in the catalog above, and check whether the caller's role grants it on the right environment. Admins can audit a user's effective permissions from `Settings → Users`.

**A user has no access at all.** They probably have no role assignments. Open them in `Settings → Users` and add at least one assignment (typically `Viewer` on every environment they should see).

**Last-admin guard tripped (`at least one user must retain a global Admin role assignment`).** You tried to remove, demote, or OIDC-resync away the last globally-scoped Admin. Add another global Admin first, then retry.

**OIDC user lost their access on login.** Their group claim no longer matches any mapping. Check the IdP-side group membership and the mapping table; OIDC-sourced assignments are re-evaluated on every login.

**API key fails with permission denied.** Keys carry their own permissions, not the owner's current permissions. If you changed the owner's role, the key still has whatever it was provisioned with. Re-issue the key with the desired scope.
