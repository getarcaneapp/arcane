<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { groupService } from '#lib/services/group-service.js';
	import type { UserRoleAssignmentInput } from '#lib/types/role.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import GroupEditorPage from '../components/group-editor-page.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let isLoading = $state(false);

	async function handleSubmit(payload: {
		name: string;
		description: string | null;
		memberUserIds: string[];
		roleAssignments: UserRoleAssignmentInput[];
	}) {
		isLoading = true;
		const safeName = payload.name.trim() || data.group.name || m.common_unknown();
		const result = await tryCatch(
			(async () => {
				const patch = changedFields(
					{ name: data.group.name, description: data.group.description ?? null },
					{ name: payload.name, description: payload.description }
				);
				if (Object.keys(patch).length > 0) {
					await groupService.update(data.group.id, patch);
				}
				await groupService.setMembers(data.group.id, { userIds: payload.memberUserIds });
				await groupService.setRoleAssignments(data.group.id, { assignments: payload.roleAssignments });
			})()
		);

		await handleApiResultWithCallbacks({
			result,
			message: m.common_update_failed({ resource: `${m.group()} "${safeName}"` }),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async () => {
				await queryClient.invalidateQueries({ queryKey: queryKeys.groups.root });
				toast.success(m.common_update_success({ resource: `${m.group()} "${safeName}"` }));
				await goto(resolve('/(app)/system/groups'));
			}
		});
	}
</script>

{#key data.group.id}
	<GroupEditorPage
		title={m.edit_group()}
		group={data.group}
		members={data.members}
		roleAssignments={data.roleAssignments}
		users={data.users}
		roles={data.roles}
		environments={data.environments}
		{isLoading}
		onSubmit={handleSubmit}
	/>
{/key}
