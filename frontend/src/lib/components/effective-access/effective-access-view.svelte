<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ShieldAlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { BindingRef, EffectivePermission, EffectiveScope } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { selectorSummary } from '#lib/utils/rbac.js';

	let { userId, environments }: { userId: string; environments: Environment[] } = $props();

	const accessQuery = createQuery(() => ({
		queryKey: queryKeys.bindings.effectiveAccess(userId),
		queryFn: () => roleService.getUserEffectiveAccess(userId)
	}));

	const access = $derived(accessQuery.data);
	const scopes = $derived((access?.scopes ?? []).filter((scope) => scope.permissions.length > 0));

	function scopeTitle(scope: EffectiveScope): string {
		if (!scope.environmentId) return m.global_org_wide();
		return environments.find((environment) => environment.id === scope.environmentId)?.name ?? scope.environmentId;
	}

	function viaLabel(ref: BindingRef): string {
		return ref.groupName ? m.effective_access_via_group({ role: ref.roleName, group: ref.groupName }) : ref.roleName;
	}

	function resourceSummary(permission: EffectivePermission): string[] {
		if (permission.unrestricted) return [];
		return (permission.selectors ?? []).flatMap((selector) => selectorSummary(selector));
	}
</script>

<div class="space-y-3">
	<div class="px-1">
		<h3 class="text-sm font-semibold">{m.effective_access_title()}</h3>
		<p class="text-xs text-muted-foreground">{m.effective_access_description()}</p>
	</div>

	{#if accessQuery.isPending}
		<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
	{:else if accessQuery.isError}
		<p class="rounded-md border border-dashed p-4 text-sm text-destructive">
			{m.common_load_failed({ resource: m.effective_access_title() })}: {extractApiErrorMessage(accessQuery.error)}
		</p>
	{:else if access?.isGlobalAdmin}
		<Alert.Root variant="info">
			<ShieldAlertIcon class="size-4" />
			<Alert.Title>{m.effective_access_admin_title()}</Alert.Title>
			<Alert.Description>{m.effective_access_admin_description()}</Alert.Description>
		</Alert.Root>
	{:else if scopes.length === 0}
		<p class="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">{m.effective_access_empty()}</p>
	{:else}
		{#each scopes as scope (scope.environmentId ?? 'global')}
			<div class="space-y-2 rounded-md border">
				<div class="flex items-center justify-between gap-2 border-b px-4 py-2">
					<span class="text-sm font-medium">{scopeTitle(scope)}</span>
					<Badge variant="gray" size="sm">{m.effective_access_permission_count({ count: scope.permissions.length })}</Badge>
				</div>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>{m.effective_access_col_permission()}</Table.Head>
							<Table.Head>{m.bindings_col_resources()}</Table.Head>
							<Table.Head>{m.effective_access_col_via()}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each scope.permissions as permission (permission.permission)}
							{@const summary = resourceSummary(permission)}
							<Table.Row>
								<Table.Cell><code class="text-xs">{permission.permission}</code></Table.Cell>
								<Table.Cell>
									{#if summary.length === 0}
										<span class="text-xs text-muted-foreground">{m.bindings_all_resources()}</span>
									{:else}
										<div class="flex flex-wrap gap-1">
											{#each summary as entry, index (`${index}-${entry}`)}
												<Badge variant="outline" size="sm" mono>{entry}</Badge>
											{/each}
										</div>
									{/if}
								</Table.Cell>
								<Table.Cell>
									<div class="flex flex-wrap gap-1">
										{#each permission.via as ref (ref.id)}
											<Badge variant={ref.groupName ? 'purple' : 'blue'} size="sm">{viaLabel(ref)}</Badge>
										{/each}
									</div>
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		{/each}
	{/if}
</div>
