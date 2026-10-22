<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { groupService } from '#lib/services/group-service.js';
	import { userService } from '#lib/services/user-service.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		groupId,
		existingMemberIds,
		onAdded
	}: {
		open: boolean;
		groupId: string;
		existingMemberIds: string[];
		onAdded: () => Promise<void>;
	} = $props();

	const userListOptions = { pagination: { page: 1, limit: -1 }, sort: { column: 'Username', direction: 'asc' as const } };

	const usersQuery = createQuery(() => ({
		queryKey: queryKeys.users.list(userListOptions),
		queryFn: () => userService.getUsers(userListOptions),
		enabled: open
	}));

	let query = $state('');
	let selected = $state<string[]>([]);
	let isSaving = $state(false);

	const candidates = $derived(
		(usersQuery.data?.data ?? [])
			.filter((user) => !existingMemberIds.includes(user.id))
			.filter((user) => {
				const needle = query.trim().toLowerCase();
				return !needle || user.username.toLowerCase().includes(needle) || (user.displayName ?? '').toLowerCase().includes(needle);
			})
	);

	function toggle(userId: string, checked: boolean) {
		selected = checked ? [...selected, userId] : selected.filter((id) => id !== userId);
	}

	function handleOpenChange(next: boolean) {
		open = next;
		if (!next) {
			selected = [];
			query = '';
		}
	}

	async function handleSubmit() {
		if (selected.length === 0) return;
		await handleApiResultWithCallbacks({
			result: await tryCatch(groupService.addMembers(groupId, selected)),
			message: m.common_update_failed({ resource: m.groups_col_members() }),
			setLoadingState: (value) => (isSaving = value),
			onSuccess: async () => {
				toast.success(m.groups_members_added({ count: selected.length }));
				handleOpenChange(false);
				await onAdded();
			}
		});
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.groups_members_add()}
	description={m.groups_members_add_description()}
	contentClass="sm:max-w-125"
>
	{#snippet children()}
		<div class="grid gap-4 py-6">
			<Input type="text" placeholder={m.common_search()} bind:value={query} disabled={isSaving} />
			{#if usersQuery.isPending}
				<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
			{:else if usersQuery.isError}
				<p class="text-sm text-destructive">
					{m.common_load_failed({ resource: m.users_title() })}: {extractApiErrorMessage(usersQuery.error)}
				</p>
			{:else if candidates.length === 0}
				<p class="text-sm text-muted-foreground">{m.groups_no_users_available()}</p>
			{:else}
				<div class="max-h-96 space-y-1 overflow-y-auto rounded-md border p-2">
					{#each candidates as user (user.id)}
						<label class="flex cursor-pointer items-center gap-3 rounded-md p-2 hover:bg-accent/40">
							<Checkbox
								checked={selected.includes(user.id)}
								disabled={isSaving}
								onCheckedChange={(checked) => toggle(user.id, checked === true)}
							/>
							<span class="flex flex-col">
								<span class="text-sm font-medium">{user.username}</span>
								{#if user.displayName}
									<span class="text-xs text-muted-foreground">{user.displayName}</span>
								{/if}
							</span>
						</label>
					{/each}
				</div>
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			onCancel={() => handleOpenChange(false)}
			cancelDisabled={isSaving}
			submitAction="create"
			submitDisabled={isSaving || selected.length === 0}
			submitLoading={isSaving}
			onSubmit={handleSubmit}
			submitLabel={m.groups_members_add()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
