import { refreshAll } from "$app/navigation";
import { page } from "$app/state";
import { createQuery, useQueryClient } from "@tanstack/svelte-query";

import { environmentSettingsAcrossQuery } from "#lib/query/environments.js";
import { queryKeys } from "#lib/query/query-keys.js";
import { settingsService } from "#lib/services/settings-service.js";
import {
  environmentStore,
  isLocalDockerDisabled,
} from "#lib/stores/environment.store.svelte.js";
import type { SettingKey, Settings } from "#lib/types/settings.js";
import type { ResolvedSettings } from "#lib/types/ui/settings.js";
import { hasPermission } from "#lib/utils/auth.js";
import { isEnvironmentOnline } from "#lib/utils/docker.js";
import { settleAcrossEnvironments } from "#lib/utils/environments.js";

export function useEnvironmentSettingsScope(permissions: () => string[]) {
  const queryClient = useQueryClient();
  let revision = $state(0);
  const selectedId = $derived(page.url.searchParams.get("env") ?? "");
  const readable = $derived(
    environmentStore.enabled.filter((env) =>
      ["environment-settings:read", ...permissions()].every((permission) =>
        hasPermission(permission, env.id),
      ),
    ),
  );
  const candidates = $derived(
    environmentStore.available.filter(
      (env) =>
        readable.some((ref) => ref.id === env.id) &&
        (!selectedId || env.id === selectedId),
    ),
  );
  // Settings need no Docker, so a manager without local Docker loads whatever status its last check saved.
  const online = $derived(
    candidates.filter(
      (env) => isEnvironmentOnline(env) || isLocalDockerDisabled(env),
    ),
  );
  const onlineScope = $derived(online.map((env) => env.id).join(","));
  const results = createQuery(() => {
    const environments = online;
    return {
      ...environmentSettingsAcrossQuery(queryClient, environments),
      enabled: environments.length > 0,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    };
  });
  const pending = $derived(online.length > 0 && results.isPending);
  const entries = $derived(
    online.flatMap((environment) => {
      const settings = results.data?.byEnvironment.find(
        (entry) => entry.environment.id === environment.id,
      )?.data;
      return settings ? [{ environment, settings }] : [];
    }),
  );
  const first = $derived(entries[0]?.settings);
  const mixedKeys = $derived(
    new Set(
      (Object.keys(first?.values ?? {}) as SettingKey[]).filter(
        (key) =>
          first?.meta[key]?.scope === "environment" &&
          entries.some(
            ({ settings }) =>
              JSON.stringify(settings.values[key]) !==
              JSON.stringify(first.values[key]),
          ),
      ),
    ),
  );

  function environmentScoped(
    settings: ResolvedSettings,
    changes: Partial<Settings>,
  ): Partial<Settings> {
    return Object.fromEntries(
      Object.entries(changes).filter(
        ([key]) => settings.meta[key as SettingKey]?.scope === "environment",
      ),
    ) as Partial<Settings>;
  }

  async function save(changes: Partial<Settings>) {
    const managerEnvironmentId = entries[0]?.environment.id;
    await settleAcrossEnvironments(
      entries.map(({ environment }) => environment),
      (environmentId) => {
        const settings = entries.find(
          ({ environment }) => environment.id === environmentId,
        )!.settings;
        return settingsService.updateSettingsForEnvironment(
          environmentId,
          settings,
          environmentId === managerEnvironmentId
            ? changes
            : environmentScoped(settings, changes),
        );
      },
    );
    await Promise.all(
      entries.map(({ environment }) =>
        queryClient.invalidateQueries({
          queryKey: queryKeys.environments.settings(environment.id),
        }),
      ),
    );
    await queryClient.invalidateQueries({ queryKey: queryKeys.jobs.all });
    await queryClient.invalidateQueries({
      queryKey: queryKeys.environments.settingsAcross(onlineScope),
    });
    await queryClient.invalidateQueries({
      queryKey: queryKeys.environments.all,
    });
    await refreshAll();
    revision++;
  }

  return {
    get selectedId() {
      return selectedId;
    },
    get environments() {
      return readable;
    },
    get entries() {
      return entries;
    },
    get isLoading() {
      return pending;
    },
    get offlineCount() {
      return candidates.length - entries.length - (pending ? online.length : 0);
    },
    get primaryId() {
      return entries[0]?.environment.id ?? selectedId;
    },
    get version() {
      return `${selectedId}:${revision}`;
    },
    get settings() {
      return first;
    },
    get values() {
      return first?.values;
    },
    mixed: (key: SettingKey) => mixedKeys.has(key),
    lock: (key: SettingKey) =>
      entries.find(({ settings }) => settings.meta[key]?.locked)?.settings.meta[
        key
      ],
    save,
  };
}
