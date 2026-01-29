<script lang="ts">
	import type { FileEntry } from '$lib/types/file-browser.type';
	import {
		FolderOpenIcon,
		FileTextIcon,
		DownloadIcon,
		TrashIcon,
		EllipsisIcon,
		EyeOnIcon,
		ClockIcon,
		RestartIcon,
		ExternalLinkIcon
	} from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '$lib/components/arcane-table';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import bytes from 'bytes';
	import { format } from 'date-fns';

	let {
		files,
		currentPath,
		persistKey = 'arcane-file-browser-table',
		onNavigate,
		onRefresh,
		onDelete,
		onDownload,
		onPreview,
		onRestoreFromBackup
	}: {
		files: FileEntry[];
		currentPath: string;
		persistKey?: string;
		onNavigate: (path: string) => void;
		onRefresh: () => void;
		onDelete: (file: FileEntry) => Promise<void>;
		onDownload: (file: FileEntry) => Promise<void>;
		onPreview: (file: FileEntry) => void;
		onRestoreFromBackup?: (file: FileEntry) => void;
	} = $props();

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 100 },
		sort: { column: 'name', direction: 'asc' }
	});

	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	function compareByColumn(a: FileEntry, b: FileEntry, column: string): number {
		switch (column) {
			case 'size':
				return Number(a.size) - Number(b.size);
			case 'modTime':
				return new Date(a.modTime).getTime() - new Date(b.modTime).getTime();
			case 'name':
			default:
				return a.name.localeCompare(b.name);
		}
	}

	const sortedFiles = $derived.by(() => {
		const sortColumn = requestOptions?.sort?.column ?? 'name';
		const direction = requestOptions?.sort?.direction === 'desc' ? -1 : 1;
		const items = files.slice();
		return items.sort((a, b) => {
			if (a.isDirectory !== b.isDirectory) {
				return a.isDirectory ? -1 : 1;
			}
			const diff = compareByColumn(a, b, sortColumn);
			if (diff !== 0) return diff * direction;
			return a.name.localeCompare(b.name);
		});
	});

	const itemsPaginated = $derived<Paginated<FileEntry & { id: string }>>({
		data: sortedFiles.map((f) => ({ ...f, id: f.path })),
		pagination: {
			currentPage: 1,
			totalPages: 1,
			totalItems: sortedFiles.length,
			itemsPerPage: sortedFiles.length
		}
	});

	function formatBytes(value: number) {
		return bytes.format(value, { unitSeparator: ' ' }) ?? '-';
	}

	async function handleDelete(file: FileEntry) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: file.name }),
			message: m.volumes_browser_delete_confirm({ name: file.name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					try {
						await onDelete(file);
						toast.success(m.common_delete_success({ resource: file.name }));
						onRefresh();
					} catch (e: any) {
						toast.error(e.message || m.common_delete_failed({ resource: file.name }));
					}
				}
			}
		});
	}

	async function handleDownload(file: FileEntry) {
		try {
			await onDownload(file);
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
		}
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'modTime', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<FileEntry & { id: string }>[];

	const mobileFields = [
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'modTime', label: m.common_created(), defaultVisible: false }
	];
</script>

{#snippet NameCell({ item }: { item: FileEntry })}
	<div class="flex items-center gap-2">
		{#if item.isDirectory}
			<FolderOpenIcon class="size-4 text-blue-500" />
			<button class="text-left font-medium hover:underline" onclick={() => onNavigate(item.path)}>
				{item.name}
			</button>
		{:else}
			<FileTextIcon class="text-muted-foreground size-4" />
			<span class="font-medium">{item.name}</span>
		{/if}
		{#if item.isSymlink}
			<Tooltip.Provider>
				<Tooltip.Root>
					<Tooltip.Trigger>
						<span class="inline-flex items-center gap-1 text-xs">
							<ExternalLinkIcon class="size-3 text-purple-500" />
							{#if item.linkTarget === '(external)'}
								<span class="text-amber-500">(external)</span>
							{/if}
						</span>
					</Tooltip.Trigger>
					<Tooltip.Content>
						{#if item.linkTarget === '(external)'}
							<p>{m.volumes_symlink_external_tooltip()}</p>
						{:else if item.linkTarget}
							<p>{m.volumes_symlink_target_tooltip({ target: item.linkTarget })}</p>
						{:else}
							<p>{m.volumes_symlink_tooltip()}</p>
						{/if}
					</Tooltip.Content>
				</Tooltip.Root>
			</Tooltip.Provider>
		{/if}
	</div>
{/snippet}

{#snippet SizeCell({ item, value }: { item: FileEntry; value: any })}
	<span class="text-muted-foreground text-sm">
		{item.isDirectory ? '--' : formatBytes(Number(value))}
	</span>
{/snippet}

{#snippet CreatedCell({ value }: { value: any })}
	<span class="text-muted-foreground text-sm">
		{format(new Date(String(value)), 'PP p')}
	</span>
{/snippet}

{#snippet RowActions({ item }: { item: FileEntry })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					size="icon"
					class="relative size-8 p-0"
					icon={EllipsisIcon}
					showLabel={false}
					customLabel={m.common_open_menu()}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				{#if !item.isDirectory}
					<DropdownMenu.Item onclick={() => onPreview(item)}>
						<EyeOnIcon class="size-4" />
						{m.common_view()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => handleDownload(item)}>
						<DownloadIcon class="size-4" />
						{m.templates_download()}
					</DropdownMenu.Item>
					{#if onRestoreFromBackup && !item.isSymlink}
						<DropdownMenu.Item onclick={() => onRestoreFromBackup(item)}>
							<RestartIcon class="size-4" />
							Restore from backup
						</DropdownMenu.Item>
					{/if}
					<DropdownMenu.Separator />
				{/if}
				<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet FileMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: FileEntry;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: item.isDirectory ? FolderOpenIcon : FileTextIcon, variant: item.isDirectory ? 'blue' : 'gray' }}
		title={(item) => item.name}
		fields={[
			{
				label: m.common_size(),
				getValue: (item) => (item.isDirectory ? '--' : formatBytes(item.size)),
				icon: EyeOnIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility.size ?? true
			}
		]}
		footer={{
			label: m.common_created(),
			getValue: (item) => format(new Date(item.modTime), 'PP p'),
			icon: ClockIcon || 'div'
		}}
		rowActions={RowActions}
		onclick={() => item.isDirectory && onNavigate(item.path)}
	/>
{/snippet}

<div class="file-browser-table bg-card overflow-hidden rounded-lg border shadow-sm">
	<ArcaneTable
		{persistKey}
		items={itemsPaginated}
		bind:requestOptions
		bind:mobileFieldVisibility
		onRefresh={async () => itemsPaginated}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={FileMobileCardSnippet}
		unstyled
		withoutPagination
		withoutSearch
		selectionDisabled
	/>
</div>

<style>
	@reference "../../../app.css";

	:global(.file-browser-table thead tr) {
		@apply bg-muted/30 hover:bg-muted/30;
	}

	:global(.file-browser-table tbody tr) {
		@apply hover:bg-muted/50 cursor-default border-none transition-colors;
	}

	:global(.file-browser-table td) {
		@apply py-2;
	}
</style>
