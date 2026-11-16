<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import OidcMappingFormSheet from '#lib/components/sheets/oidc-mapping-form-sheet.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { oidcMappingListQuery } from '#lib/query/oidc-mappings.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { oidcMappingService } from '#lib/services/oidc-mapping-service.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { OidcRoleMapping, Role } from '#lib/types/role.js';
	import type { OidcRoleMappingWritable } from '#lib/types/ui/role.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import OidcMappingTable from './oidc-mapping-table.svelte';

	interface Props {
		initialMappings: OidcRoleMapping[];
		roles: Role[];
		environments: Environment[];
	}

	let { initialMappings, roles, environments }: Props = $props();

	const queryClient = useQueryClient();
	const oidcMappingsQuery = createQuery(() => oidcMappingListQuery());
	const oidcMappings = $derived(oidcMappingsQuery.data ?? initialMappings);
	let mappingSheetOpen = $state(false);
	let editingMapping: OidcRoleMapping | null = $state(null);
	let mappingSaving = $state(false);

	function openCreateMapping() {
		editingMapping = null;
		mappingSheetOpen = true;
	}

	function openEditMapping(mapping: OidcRoleMapping) {
		editingMapping = mapping;
		mappingSheetOpen = true;
	}

	async function submitMapping(formData: Required<Pick<OidcRoleMapping, OidcRoleMappingWritable>>) {
		mappingSaving = true;

		if (editingMapping) {
			const id = editingMapping.id;
			await handleApiResultWithCallbacks({
				result: await tryCatch(
					oidcMappingService.update(
						id,
						changedFields({ ...editingMapping, environmentId: editingMapping.environmentId ?? null }, formData)
					)
				),
				message: m.common_update_failed({ resource: m.resource_oidc_mapping() }),
				setLoadingState: (v) => (mappingSaving = v),
				onSuccess: async () => {
					toast.success(m.common_update_success({ resource: m.resource_oidc_mapping_cap() }));
					await queryClient.invalidateQueries({ queryKey: queryKeys.oidcMappings.all });
					mappingSheetOpen = false;
				}
			});
		} else {
			await handleApiResultWithCallbacks({
				result: await tryCatch(oidcMappingService.create({ ...formData, environmentId: formData.environmentId ?? undefined })),
				message: m.common_create_failed({ resource: m.resource_oidc_mapping() }),
				setLoadingState: (v) => (mappingSaving = v),
				onSuccess: async () => {
					toast.success(m.common_create_success({ resource: m.resource_oidc_mapping_cap() }));
					await queryClient.invalidateQueries({ queryKey: queryKeys.oidcMappings.all });
					mappingSheetOpen = false;
				}
			});
		}
	}
</script>

<IfPermitted adminOnly>
	<SettingsSection title={m.oidc_role_mappings_title()} description={m.oidc_role_mappings_description()} variant="plain">
		{#snippet actions()}
			<ArcaneButton
				action="create"
				tone="outline"
				onclick={openCreateMapping}
				customLabel={m.common_create_button({ resource: m.resource_oidc_mapping_cap() })}
			/>
		{/snippet}
		{#if oidcMappings.length > 0}
			<OidcMappingTable
				mappings={oidcMappings}
				{roles}
				{environments}
				onRetry={() => oidcMappingsQuery.refetch()}
				onEdit={openEditMapping}
			/>
		{:else}
			<div class="rounded-xl border border-dashed border-border/60 p-6 text-center">
				<p class="text-sm text-muted-foreground">{m.oidc_mappings_empty_body()}</p>
			</div>
		{/if}
	</SettingsSection>
</IfPermitted>

{#key editingMapping?.id ?? 'new'}
	<OidcMappingFormSheet
		bind:open={mappingSheetOpen}
		mappingToEdit={editingMapping}
		isLoading={mappingSaving}
		{roles}
		{environments}
		onSubmit={submitMapping}
	/>
{/key}
