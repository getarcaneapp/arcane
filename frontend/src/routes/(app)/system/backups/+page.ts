import {
  projectBackupConfigQuery,
  s3DestinationAllQuery,
  systemBackupHistoryQuery,
  systemBackupPoliciesQuery,
  systemVolumeBackupConfigQuery,
} from "#lib/query/backups.js";
import { environmentStore } from "#lib/stores/environment.store.svelte.js";
import { GLOBAL_SCOPE } from "#lib/types/role.js";
import type { SearchPaginationSortRequest } from "#lib/types/ui/pagination.js";
import { userHasPermission } from "#lib/utils/auth.js";
import { resolveInitialTableRequest } from "#lib/utils/tables.js";

import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ parent }) => {
  const [{ queryClient, user }] = await Promise.all([
    parent(),
    environmentStore.ready,
  ]);
  const canReadBackups = userHasPermission(
    user,
    "system-backups:read",
    GLOBAL_SCOPE,
  );
  const requestOptions = resolveInitialTableRequest(
    "arcane-system-backups-table",
    {
      pagination: { page: 1, limit: 20 },
      sort: { column: "createdAt", direction: "desc" },
    } satisfies SearchPaginationSortRequest,
  );
  const [
    backups,
    policyCollection,
    destinations,
    projectPolicyCollection,
    systemVolumePolicyCollection,
  ] = await Promise.all([
    queryClient.query(
      systemBackupHistoryQuery(environmentStore.shownKey, requestOptions),
    ),
    canReadBackups
      ? queryClient.query(systemBackupPoliciesQuery())
      : { policies: [], recoveryKeyStored: false, localDockerEnabled: true },
    userHasPermission(user, "s3-destinations:list", GLOBAL_SCOPE)
      ? queryClient.query(s3DestinationAllQuery())
      : [],
    canReadBackups
      ? queryClient.query(projectBackupConfigQuery())
      : { policies: [] },
    canReadBackups
      ? queryClient.query(systemVolumeBackupConfigQuery())
      : { policies: [] },
  ]);
  return {
    backups,
    policyCollection,
    destinations,
    projectPolicyCollection,
    systemVolumePolicyCollection,
    requestOptions,
  };
};
