<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { untrack } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import RoleAssignmentsEditor from '#lib/components/forms/role-assignments-editor.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { Group, GroupMember, GroupRoleAssignment } from '#lib/types/group.js';
	import type { Role, UserRoleAssignmentInput } from '#lib/types/role.js';
	import type { User } from '#lib/types/user.js';

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
		users,
		roles,
		environments,
		isLoading = false,
		onSubmit
	}: Props = $props();

	let name = $state(untrack(() => group?.name ?? ''));
	let description = $state(untrack(() => group?.description ?? ''));
	let userSearch = $state('');

	let selectedUserIds = $state<string[]>(
		untrack(() => members.filter((member) => member.source === 'manual').map((member) => member.userId))
	);

	let manualRoleAssignments = $state<UserRoleAssignmentInput[]>(
		untrack(() =>
			roleAssignments
				.filter((assignment) => assignment.source === 'manual')
				.map((assignment) => ({
					roleId: assignment.roleId,
					environmentId: assignment.environmentId
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
										checked={isUserSelected(user.id) || synced}
										disabled={isLoading || synced}
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
									<Badge variant="gray" size="sm">IdP</Badge>
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
