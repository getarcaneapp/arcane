<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import type { S3Destination } from '#lib/types/s3-destination';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import { EditIcon, RemoteEnvironmentIcon, TrashIcon, ClockIcon, GlobeIcon, TestIcon } from '#lib/icons';
	import { formatOptionalDateTime } from '#lib/utils/formatting';
	import * as m from '#lib/paraglide/messages.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Spinner } from '#lib/components/ui/spinner';
	import { toast } from 'svelte-sonner';
	import { s3DestinationService } from '#lib/services/s3-destination-service';

	let {
		destinations = $bindable(),
		requestOptions = $bindable(),
		onDestinationsChanged,
		onEdit,
		onDelete
	}: {
		destinations: Paginated<S3Destination>;
		requestOptions: SearchPaginationSortRequest;
		onDestinationsChanged: (options: SearchPaginationSortRequest) => Promise<Paginated<S3Destination>>;
		onEdit: (destination: S3Destination) => void;
		onDelete: (destination: S3Destination) => void;
	} = $props();

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'bucket', title: m.backups_s3_bucket_label(), sortable: true },
		{ accessorKey: 'endpoint', title: m.backups_s3_endpoint_label(), sortable: true, cell: EndpointCell },
		{ accessorKey: 'region', title: m.backups_s3_region_label(), sortable: true },
		{ accessorKey: 'prefix', title: m.backups_s3_prefix_label(), sortable: true, cell: PrefixCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: UpdatedCell }
	] satisfies ColumnSpec<S3Destination>[];

	const mobileFields = [
		{ id: 'bucket', label: m.backups_s3_bucket_label(), defaultVisible: true },
		{ id: 'endpoint', label: m.backups_s3_endpoint_label(), defaultVisible: true },
		{ id: 'region', label: m.backups_s3_region_label(), defaultVisible: true },
		{ id: 'prefix', label: m.backups_s3_prefix_label(), defaultVisible: false }
	];
	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let testingId = $state<string | null>(null);

	async function testDestination(destination: S3Destination) {
		testingId = destination.id;
		try {
			await s3DestinationService.test(destination.id);
			toast.success(m.s3_destination_test_success({ name: destination.name }));
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.s3_destination_test_failed({ name: destination.name }));
		} finally {
			testingId = null;
		}
	}
</script>

{#snippet NameCell({ item }: { item: S3Destination })}
	<span class="font-medium">{item.name}</span>
{/snippet}

{#snippet EndpointCell({ item }: { item: S3Destination })}
	<span class="text-muted-foreground">{item.endpoint || m.s3_destination_aws_default()}</span>
{/snippet}

{#snippet PrefixCell({ item }: { item: S3Destination })}
	<code class="text-xs">{item.prefix || '-'}</code>
{/snippet}

{#snippet UpdatedCell({ item }: { item: S3Destination })}
	{formatOptionalDateTime(item.updatedAt ?? item.createdAt)}
{/snippet}

{#snippet RowActions({ item }: { item: S3Destination })}
	<RowActionsMenu>
		<IfPermitted perm="s3-destinations:test">
			<DropdownMenu.Item onclick={() => testDestination(item)} disabled={testingId === item.id}>
				{#if testingId === item.id}
					<Spinner class="size-4" />
				{:else}
					<TestIcon class="size-4" />
				{/if}
				{m.test_connection()}
			</DropdownMenu.Item>
		</IfPermitted>
		<IfPermitted perm="s3-destinations:update">
			<DropdownMenu.Item onclick={() => onEdit(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>
		</IfPermitted>
		<IfPermitted perm="s3-destinations:delete">
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => onDelete(item)}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

{#snippet MobileCard({ item, mobileFieldVisibility }: { item: S3Destination; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: RemoteEnvironmentIcon, variant: 'blue' }}
		title={(item) => item.name}
		fields={[
			{
				label: m.backups_s3_bucket_label(),
				getValue: (item) => item.bucket,
				icon: RemoteEnvironmentIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['bucket'] ?? true
			},
			{
				label: m.backups_s3_endpoint_label(),
				getValue: (item) => item.endpoint || m.s3_destination_aws_default(),
				icon: GlobeIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['endpoint'] ?? true
			},
			{
				label: m.backups_s3_region_label(),
				getValue: (item) => item.region,
				icon: GlobeIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['region'] ?? true
			},
			{
				label: m.backups_s3_prefix_label(),
				getValue: (item) => item.prefix || '-',
				icon: RemoteEnvironmentIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['prefix'] ?? false
			}
		]}
		footer={{
			label: m.common_updated(),
			getValue: (item) => formatOptionalDateTime(item.updatedAt ?? item.createdAt),
			icon: ClockIcon
		}}
		rowActions={RowActions}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-s3-destinations-table"
	items={destinations}
	bind:requestOptions
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		destinations = await onDestinationsChanged(options);
		return destinations;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={MobileCard}
/>
