<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import RoleAssignmentsEditor from '#lib/components/forms/role-assignments-editor.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { CloseIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { groupService } from '#lib/services/group-service.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { Group, GroupIdpMapping, GroupMember, GroupRoleAssignment } from '#lib/types/group.js';
	import type { Role, UserRoleAssignmentInput } from '#lib/types/role.js';
	import type { User } from '#lib/types/user.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type SubmitPayload = {
		name: string;
		description: string | null;
		memberUserIds: string[];
		roleAssignments: UserRoleAssignmentInput[];
	};

	type Props = {
		title: string;
		group: Group | null;
		members?: GroupMember[];
		roleAssignments?: GroupRoleAssignment[];
		idpMappings?: GroupIdpMapping[];
		users: User[];
		roles: Role[];
		environments: Environment[];
		isLoading?: boolean;
		onSubmit: (payload: SubmitPayload) => void | Promise<void>;
	};

	let {
		title,
		group,
		members = [],
		roleAssignments = [],
		idpMappings = [],
		users,
		roles,
		environments,
		isLoading = false,
		onSubmit
	}: Props = $props();

	const queryClient = useQueryClient();

	let name = $state(untrack(() => group?.name ?? ''));
	let description = $state(untrack(() => group?.description ?? ''));
	let userSearch = $state('');
	let mappings = $state<GroupIdpMapping[]>(untrack(() => [...idpMappings]));
	let newClaimValue = $state('');
	let isUpdatingMappings = $state(false);

	async function handleAddIdpMapping() {
		const claim = newClaimValue.trim();
		if (!group || !claim || isUpdatingMappings) return;
		isUpdatingMappings = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(groupService.createIdpMapping({ claimValue: claim, groupId: group.id })),
			message: m.common_create_failed({ resource: claim }),
			setLoadingState: (value) => (isUpdatingMappings = value),
			onSuccess: async (created) => {
				mappings = [...mappings, created];
				newClaimValue = '';
				await queryClient.invalidateQueries({ queryKey: queryKeys.groups.idpMappings(group.id) });
				toast.success(m.common_create_success({ resource: created.claimValue }));
			}
		});
	}

	async function handleDeleteIdpMapping(mapping: GroupIdpMapping) {
		if (!group || isUpdatingMappings) return;
		isUpdatingMappings = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(groupService.deleteIdpMapping(mapping.id)),
			message: m.common_delete_failed({ resource: mapping.claimValue }),
			setLoadingState: (value) => (isUpdatingMappings = value),
			onSuccess: async () => {
				mappings = mappings.filter((item) => item.id !== mapping.id);
				await queryClient.invalidateQueries({ queryKey: queryKeys.groups.idpMappings(group.id) });
				toast.success(m.common_delete_success({ resource: mapping.claimValue }));
			}
		});
	}

	let selectedUserIds = $state<string[]>(
		untrack(() => members.filter((member) => member.source === 'manual').map((member) => member.userId))
	);

	let manualRoleAssignments = $state<UserRoleAssignmentInput[]>(
		untrack(() =>
			roleAssignments
				.filter((assignment) => assignment.source === 'manual')
				.map((assignment) => ({
					roleId: assignment.roleId,
					environmentId: assignment.environmentId,
					resourceType: assignment.resourceType,
					resourceId: assignment.resourceId
				}))
		)
	);

	const syncedMemberIds = $derived(
		new Set(members.filter((member) => member.source !== 'manual').map((member) => member.userId))
	);

	const filteredUsers = $derived.by(() => {
		const term = userSearch.trim().toLowerCase();
		if (!term) return users;
		return users.filter(
			(user) =>
				user.username.toLowerCase().includes(term) ||
				(user.displayName ?? '').toLowerCase().includes(term) ||
				(user.email ?? '').toLowerCase().includes(term)
		);
	});

	function isUserSelected(userId: string): boolean {
		return selectedUserIds.includes(userId);
	}

	function toggleUser(userId: string, checked: boolean) {
		if (checked) {
			if (!selectedUserIds.includes(userId)) {
				selectedUserIds = [...selectedUserIds, userId];
			}
		} else {
			selectedUserIds = selectedUserIds.filter((id) => id !== userId);
		}
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const trimmedName = name.trim();
		if (!trimmedName) return;
		const trimmedDesc = description.trim();
		await onSubmit({
			name: trimmedName,
			description: trimmedDesc ? trimmedDesc : null,
			memberUserIds: selectedUserIds,
			roleAssignments: manualRoleAssignments
		});
	}
</script>

<SettingsPageLayout {title} description={m.groups_subtitle()} pageType="form">
	{#snippet mainContent()}
		<form onsubmit={handleSubmit} class="space-y-6">
			<div class="space-y-4 rounded-lg border p-4">
				<div class="grid gap-4 sm:grid-cols-2">
					<div class="space-y-2">
						<Label for="group-name">{m.common_name()}</Label>
						<Input id="group-name" bind:value={name} placeholder={m.group_name_placeholder()} required disabled={isLoading} />
					</div>
					<div class="space-y-2">
						<Label for="group-description">{m.common_description()}</Label>
						<Input
							id="group-description"
							bind:value={description}
							placeholder={m.group_description_placeholder()}
							disabled={isLoading}
						/>
					</div>
				</div>
			</div>

			<div class="space-y-3 rounded-lg border p-4">
				<div>
					<h3 class="text-sm font-medium">{m.group_members()}</h3>
					<p class="text-xs text-muted-foreground">{m.group_members_description()}</p>
				</div>

				<Input bind:value={userSearch} placeholder={m.search_users()} disabled={isLoading} />

				{#if filteredUsers.length === 0}
					<p class="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">{m.no_users_found()}</p>
				{:else}
					<div class="max-h-64 space-y-1 overflow-y-auto rounded-md border p-2">
						{#each filteredUsers as user (user.id)}
							{@const synced = syncedMemberIds.has(user.id)}
							<label class="flex cursor-pointer items-center justify-between gap-3 rounded-md px-2 py-1.5 hover:bg-accent/40">
								<div class="flex min-w-0 items-center gap-3">
									<Checkbox
										checked={isUserSelected(user.id)}
										disabled={isLoading}
										onCheckedChange={(checked) => toggleUser(user.id, checked === true)}
									/>
									<div class="min-w-0">
										<div class="truncate text-sm font-medium">{user.username}</div>
										{#if user.displayName || user.email}
											<div class="truncate text-xs text-muted-foreground">
												{[user.displayName, user.email].filter(Boolean).join(' · ')}
											</div>
										{/if}
									</div>
								</div>
								{#if synced}
									<Badge variant="gray" size="sm">{m.idp()}</Badge>
								{/if}
							</label>
						{/each}
					</div>
				{/if}
			</div>

			<div class="space-y-3 rounded-lg border p-4">
				<div>
					<h3 class="text-sm font-medium">{m.group_role_assignments()}</h3>
					<p class="text-xs text-muted-foreground">{m.group_role_assignments_description()}</p>
				</div>

				<RoleAssignmentsEditor bind:assignments={manualRoleAssignments} {roles} {environments} disabled={isLoading} />
			</div>

			{#if group}
				<div class="space-y-3 rounded-lg border p-4">
					<div>
						<h3 class="text-sm font-medium">{m.group_idp_mappings()}</h3>
						<p class="text-xs text-muted-foreground">{m.group_idp_mappings_description()}</p>
					</div>

					<div class="flex gap-2">
						<Input
							bind:value={newClaimValue}
							placeholder={m.group_idp_claim_placeholder()}
							disabled={isLoading || isUpdatingMappings}
							onkeydown={(e) => {
								if (e.key === 'Enter') {
									e.preventDefault();
									handleAddIdpMapping();
								}
							}}
						/>
						<ArcaneButton
							action="base"
							tone="outline"
							type="button"
							disabled={isLoading || isUpdatingMappings || !newClaimValue.trim()}
							loading={isUpdatingMappings}
							onclick={handleAddIdpMapping}
							customLabel={m.add_claim_mapping()}
						/>
					</div>

					{#if mappings.length === 0}
						<p class="rounded-md border border-dashed p-3 text-center text-xs text-muted-foreground">
							{m.no_group_idp_mappings()}
						</p>
					{:else}
						<div class="flex flex-wrap gap-2">
							{#each mappings as mapping (mapping.id)}
								<Badge variant="outline">
									<span>{mapping.claimValue}</span>
									{#if mapping.source === 'manual'}
										<button
											type="button"
											class="ml-1 rounded-sm p-0.5 text-muted-foreground hover:text-destructive"
											aria-label={m.common_remove()}
											disabled={isLoading || isUpdatingMappings}
											onclick={() => handleDeleteIdpMapping(mapping)}
										>
											<CloseIcon class="size-3" />
										</button>
									{/if}
								</Badge>
							{/each}
						</div>
					{/if}
				</div>
			{/if}

			<div class="flex items-center justify-end gap-2">
				<ArcaneButton
					action="cancel"
					tone="outline"
					type="button"
					disabled={isLoading}
					onclick={() => goto(resolve('/(app)/system/groups'))}
				/>
				<ArcaneButton action="save" type="submit" loading={isLoading} disabled={isLoading || !name.trim()} />
			</div>
		</form>
	{/snippet}
</SettingsPageLayout>
