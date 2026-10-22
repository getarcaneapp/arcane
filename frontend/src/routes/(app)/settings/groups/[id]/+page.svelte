<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import BindingsTable from '#lib/components/role-bindings/bindings-table.svelte';
	import GroupFormSheet from '#lib/components/sheets/group-form-sheet.svelte';
	import { UsersIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { groupService } from '#lib/services/group-service.js';
	import type { GroupCreate, GroupMember } from '#lib/types/auth.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import AddMembersSheet from './components/add-members-sheet.svelte';
	import GroupMembersTable from './components/group-members-table.svelte';

	let { data } = $props();

	const queryClient = useQueryClient();
	const groupId = $derived(data.group.id);

	const groupQuery = createQuery(() => ({
		queryKey: queryKeys.groups.detail(groupId),
		queryFn: () => groupService.get(groupId),
		initialData: data.group
	}));

	const membersQuery = createQuery(() => ({
		queryKey: queryKeys.groups.members(groupId),
		queryFn: () => groupService.getMembers(groupId),
		initialData: data.members
	}));

	const group = $derived(groupQuery.data ?? data.group);
	const members = $derived(membersQuery.data ?? data.members);

	let editOpen = $state(false);
	let addMembersOpen = $state(false);
	let isSaving = $state(false);
	let removingMember = $state(false);

	async function refreshGroup() {
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: queryKeys.groups.all }),
			queryClient.invalidateQueries({ queryKey: queryKeys.bindings.all })
		]);
	}

	async function saveGroup(values: Required<GroupCreate>) {
		const safeName = values.name || group.name;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				groupService.update(group.id, changedFields({ ...group, description: group.description ?? null }, values))
			),
			message: m.common_update_failed({ resource: `${m.resource_group()} "${safeName}"` }),
			setLoadingState: (value) => (isSaving = value),
			onSuccess: async () => {
				toast.success(m.common_update_success({ resource: `${m.resource_group_cap()} "${safeName}"` }));
				editOpen = false;
				await refreshGroup();
			}
		});
	}

	function deleteGroup() {
		confirmAndRun({
			title: m.groups_delete_title({ name: group.name }),
			message: m.groups_delete_message({ name: group.name }),
			confirmLabel: m.common_delete(),
			destructive: true,
			setLoading: (value) => (isSaving = value),
			run: () => groupService.delete(group.id),
			failureMessage: m.common_delete_failed({ resource: `${m.resource_group()} "${group.name}"` }),
			onSuccess: async () => {
				toast.success(m.common_delete_success({ resource: `${m.resource_group_cap()} "${group.name}"` }));
				await queryClient.invalidateQueries({ queryKey: queryKeys.groups.all });
				await goto('/settings/groups');
			}
		});
	}

	function removeMember(member: GroupMember) {
		confirmAndRun({
			title: m.groups_members_remove_title({ name: member.username }),
			message: m.groups_members_remove_message({ name: member.username }),
			confirmLabel: m.common_remove(),
			destructive: true,
			setLoading: (value) => (removingMember = value),
			run: () => groupService.removeMember(group.id, member.userId),
			failureMessage: m.common_remove_failed({ resource: member.username }),
			onSuccess: async () => {
				toast.success(m.common_remove_success({ resource: member.username }));
				await refreshGroup();
			}
		});
	}

	const actionButtons: ActionButton[] = $derived.by(() => [
		...(hasPermission(PERMISSIONS.GROUPS_UPDATE)
			? [
					{
						id: 'edit',
						action: 'edit' as const,
						placement: 'primary' as const,
						label: m.common_edit(),
						onclick: () => (editOpen = true),
						disabled: isSaving
					}
				]
			: []),
		...(hasPermission(PERMISSIONS.GROUPS_DELETE)
			? [
					{
						id: 'delete',
						action: 'remove' as const,
						group: 'danger' as const,
						destructive: true,
						label: m.common_delete(),
						onclick: deleteGroup,
						loading: isSaving,
						disabled: isSaving
					}
				]
			: [])
	]);
</script>

<SettingsPageLayout
	title={group.name}
	description={group.description || m.groups_subtitle()}
	icon={UsersIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<div class="grid gap-8">
			<div class="space-y-3">
				<div class="flex items-start justify-between gap-4">
					<div class="px-1">
						<h3 class="text-sm font-semibold">{m.groups_col_members()}</h3>
						<p class="text-xs text-muted-foreground">{m.groups_members_description()}</p>
					</div>
					<IfPermitted perm={PERMISSIONS.GROUPS_MEMBERS}>
						<ArcaneButton
							action="create"
							size="sm"
							onclick={() => (addMembersOpen = true)}
							customLabel={m.groups_members_add()}
						/>
					</IfPermitted>
				</div>
				{#if members.length === 0}
					<p class="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">{m.groups_members_empty()}</p>
				{:else}
					<GroupMembersTable {members} removing={removingMember} onRemove={removeMember} onRefresh={refreshGroup} />
				{/if}
			</div>

			<IfPermitted perm={PERMISSIONS.BINDINGS_LIST}>
				<BindingsTable
					subjectType="group"
					subjectId={group.id}
					subjectName={group.name}
					roles={data.roles}
					environments={data.environments}
					onChanged={refreshGroup}
				/>
			</IfPermitted>
		</div>
	{/snippet}

	{#snippet additionalContent()}
		<GroupFormSheet bind:open={editOpen} groupToEdit={group} isLoading={isSaving} onSubmit={saveGroup} />
		<AddMembersSheet
			bind:open={addMembersOpen}
			groupId={group.id}
			existingMemberIds={members.map((member) => member.userId)}
			onAdded={refreshGroup}
		/>
	{/snippet}
</SettingsPageLayout>
