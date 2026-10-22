<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RelativeTimeCell from '#lib/components/arcane-table/cells/relative-time-cell.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { TrashIcon, UserIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import type { GroupMember } from '#lib/types/auth.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';

	let {
		members,
		removing,
		onRemove,
		onRefresh
	}: {
		members: GroupMember[];
		removing: boolean;
		onRemove: (member: GroupMember) => void;
		onRefresh: () => Promise<void>;
	} = $props();

	type MemberRow = GroupMember & { id: string };

	const paginatedMembers = $derived.by<Paginated<MemberRow>>(() => ({
		data: members.map((member) => ({ ...member, id: `${member.userId}:${member.source}` })),
		pagination: {
			totalPages: 1,
			totalItems: members.length,
			currentPage: 1,
			itemsPerPage: Math.max(members.length, 1)
		}
	}));

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 1000 },
		sort: { column: 'username', direction: 'asc' }
	});
	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	function sourceLabel(member: MemberRow): string {
		return member.source === 'oidc' ? m.groups_member_source_oidc() : m.groups_member_source_manual();
	}

	const columns = [
		{ accessorKey: 'username', title: m.common_username(), sortable: true, cell: UsernameCell },
		{ accessorKey: 'displayName', title: m.common_display_name(), sortable: true },
		{ accessorKey: 'source', title: m.bindings_col_source(), sortable: true, cell: SourceCell },
		{ accessorKey: 'createdAt', title: m.groups_col_member_since(), sortable: true, cellComponent: RelativeTimeCell }
	] satisfies ColumnSpec<MemberRow>[];

	const mobileFields = [
		{ id: 'displayName', label: m.common_display_name(), defaultVisible: true },
		{ id: 'source', label: m.bindings_col_source(), defaultVisible: true }
	];
</script>

{#snippet UsernameCell({ item }: { item: MemberRow })}
	<span class="font-medium">{item.username}</span>
{/snippet}

{#snippet SourceCell({ item }: { item: MemberRow })}
	<Badge variant={item.source === 'oidc' ? 'purple' : 'gray'} size="sm">{sourceLabel(item)}</Badge>
{/snippet}

{#snippet RowActions({ item }: { item: MemberRow })}
	<IfPermitted perm={PERMISSIONS.GROUPS_MEMBERS}>
		<RowActionsMenu>
			<DropdownMenu.Item variant="destructive" disabled={removing} onclick={() => onRemove(item)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		</RowActionsMenu>
	</IfPermitted>
{/snippet}

{#snippet MemberMobileCard({ item, mobileFieldVisibility }: { item: MemberRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: UserIcon, variant: 'blue' }}
		title={(item: MemberRow) => item.username}
		subtitle={(item: MemberRow) => ((mobileFieldVisibility['displayName'] ?? true) ? (item.displayName ?? null) : null)}
		badges={[
			(item: MemberRow) =>
				(mobileFieldVisibility['source'] ?? true)
					? { variant: item.source === 'oidc' ? 'purple' : 'gray', text: sourceLabel(item) }
					: null
		]}
		rowActions={RowActions}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-group-members-table"
	items={paginatedMembers}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	selectionDisabled
	withoutPagination
	onRefresh={async () => {
		await onRefresh();
		return paginatedMembers;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={MemberMobileCard}
/>
