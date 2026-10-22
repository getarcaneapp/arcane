<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import RoleScopeSelects from '#lib/components/sheets/role-scope-selects.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { InfoIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { projectService } from '#lib/services/project-service.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { BindingSubjectType, Role, RoleBindingCreateResult } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		buildGlobalEnvironmentOptions,
		createRoleEnvironmentLabelers,
		GLOBAL_ENVIRONMENT_OPTION_ID
	} from '#lib/utils/options.js';
	import {
		emptySelectorDraft,
		selectorFromDraft,
		selectorKindsByPermission,
		selectorKindsForPermissions
	} from '#lib/utils/rbac.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SelectorEditor from './selector-editor.svelte';

	let {
		open = $bindable(false),
		subjectType,
		subjectId,
		subjectName,
		roles,
		environments,
		onCreated
	}: {
		open: boolean;
		subjectType: BindingSubjectType;
		subjectId: string;
		subjectName: string;
		roles: Role[];
		environments: Environment[];
		onCreated: (result: RoleBindingCreateResult) => void | Promise<void>;
	} = $props();

	let roleId = $state('');
	let environmentValue = $state(GLOBAL_ENVIRONMENT_OPTION_ID);
	let restrict = $state(false);
	let draft = $state(emptySelectorDraft());
	let roleError = $state<string | null>(null);
	let isSaving = $state(false);

	const envOptions = $derived(buildGlobalEnvironmentOptions(environments, m.global_org_wide()));
	const selectedLabel = $derived(createRoleEnvironmentLabelers(roles, envOptions, m.common_select_option()));
	const selectedRole = $derived(roles.find((role) => role.id === roleId));
	const environmentId = $derived(environmentValue === GLOBAL_ENVIRONMENT_OPTION_ID ? undefined : environmentValue);
	const manifestQuery = createQuery(() => ({
		queryKey: queryKeys.roles.permissionsManifest(),
		queryFn: () => roleService.getPermissionsManifest(),
		enabled: open
	}));

	const kindsByPermission = $derived(selectorKindsByPermission(manifestQuery.data));
	const kinds = $derived(selectorKindsForPermissions(kindsByPermission, selectedRole?.permissions ?? []));
	const droppedPreview = $derived(
		(selectedRole?.permissions ?? []).filter(
			(permission) => selectorKindsForPermissions(kindsByPermission, [permission]).length === 0
		)
	);
	const selector = $derived(restrict ? selectorFromDraft(draft, kinds) : undefined);
	const selectorMissing = $derived(restrict && !selector);
	const loadProjects = $derived(
		open && restrict && !!environmentId && kinds.includes('projects') && hasPermission(PERMISSIONS.PROJECTS_LIST, environmentId)
	);

	const projectListOptions = { pagination: { page: 1, limit: -1 } };

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(environmentId ?? '', projectListOptions),
		queryFn: () => projectService.getProjectsForEnvironment(environmentId ?? '', projectListOptions),
		enabled: loadProjects,
		staleTime: 30_000
	}));

	const projectOptions = $derived(loadProjects ? (projectsQuery.data?.data ?? []).map((project) => project.name) : []);

	function reset() {
		roleId = '';
		environmentValue = GLOBAL_ENVIRONMENT_OPTION_ID;
		restrict = false;
		draft = emptySelectorDraft();
		roleError = null;
	}

	function handleOpenChange(next: boolean) {
		open = next;
		if (!next) reset();
	}

	async function handleSubmit() {
		if (!selectedRole) {
			roleError = m.bindings_role_required();
			return;
		}
		roleError = null;
		if (selectorMissing) return;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				roleService.createBinding({
					subjectType,
					subjectId,
					roleId: selectedRole.id,
					environmentId,
					selector
				})
			),
			message: m.common_create_failed({ resource: m.resource_binding() }),
			setLoadingState: (value) => (isSaving = value),
			onSuccess: async (result) => {
				toast.success(m.common_create_success({ resource: m.resource_binding_cap() }));
				if (result.droppedPermissions.length > 0) {
					toast.warning(m.bindings_dropped_title(), {
						description: m.bindings_dropped_description({ permissions: result.droppedPermissions.join(', ') })
					});
				}
				handleOpenChange(false);
				await onCreated(result);
			}
		});
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.bindings_create_title()}
	description={m.bindings_create_description({ subject: subjectName })}
	contentClass="sm:max-w-160"
>
	{#snippet children()}
		<div class="grid gap-6 py-6">
			<RoleScopeSelects
				idPrefix="binding"
				roleLabel={m.common_role()}
				scopeLabel={m.bindings_scope_label()}
				{roles}
				{envOptions}
				bind:roleValue={roleId}
				bind:environmentValue
				{roleError}
				roleSelectedLabel={selectedLabel.role}
				envSelectedLabel={selectedLabel.environment}
				disabled={isSaving}
			/>

			<div class="space-y-4 rounded-md border p-4">
				<label class="flex items-start justify-between gap-4">
					<span class="flex flex-col gap-0.5">
						<span class="text-sm font-medium">{m.bindings_restrict_label()}</span>
						<span class="text-xs text-muted-foreground">{m.bindings_restrict_description()}</span>
					</span>
					<Switch bind:checked={restrict} disabled={isSaving || !selectedRole || !manifestQuery.data} />
				</label>

				{#if manifestQuery.isError}
					<p class="text-sm text-destructive">
						{m.common_load_failed({ resource: m.permissions() })}: {extractApiErrorMessage(manifestQuery.error)}
					</p>
				{:else if restrict && selectedRole && manifestQuery.data}
					<SelectorEditor bind:draft {kinds} {projectOptions} disabled={isSaving} />
					{#if selectorMissing && kinds.length > 0}
						<p class="text-sm text-destructive">{m.bindings_selector_required()}</p>
					{/if}
					{#if droppedPreview.length > 0 && kinds.length > 0}
						<Alert.Root variant="warning-subtle">
							<InfoIcon class="size-4 text-warning" />
							<Alert.Title>{m.bindings_dropped_title()}</Alert.Title>
							<Alert.Description>
								{m.bindings_dropped_description({ permissions: droppedPreview.join(', ') })}
							</Alert.Description>
						</Alert.Root>
					{/if}
				{/if}
			</div>
		</div>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			onCancel={() => handleOpenChange(false)}
			cancelDisabled={isSaving}
			submitAction="create"
			submitDisabled={isSaving || selectorMissing}
			submitLoading={isSaving}
			onSubmit={handleSubmit}
			submitLabel={m.bindings_add()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
