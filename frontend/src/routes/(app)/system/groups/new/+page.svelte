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
		const safeName = payload.name.trim() || m.common_unknown();
		const result = await tryCatch(
			(async () => {
				const created = await groupService.create({
					name: payload.name,
					description: payload.description
				});
				if (payload.memberUserIds.length > 0) {
					await groupService.setMembers(created.id, { userIds: payload.memberUserIds });
				}
				if (payload.roleAssignments.length > 0) {
					await groupService.setRoleAssignments(created.id, { assignments: payload.roleAssignments });
				}
				return created;
			})()
		);

		await handleApiResultWithCallbacks({
			result,
			message: m.common_create_failed({ resource: `${m.group()} "${safeName}"` }),
			setLoadingState: (value) => (isLoading = value),
			onSuccess: async () => {
				await queryClient.invalidateQueries({ queryKey: queryKeys.groups.root });
				toast.success(m.common_create_success({ resource: `${m.group()} "${safeName}"` }));
				await goto(resolve('/(app)/system/groups'));
			}
		});
	}
</script>

<GroupEditorPage
	title={m.create_group()}
	group={null}
	users={data.users}
	roles={data.roles}
	environments={data.environments}
	{isLoading}
	onSubmit={handleSubmit}
/>
