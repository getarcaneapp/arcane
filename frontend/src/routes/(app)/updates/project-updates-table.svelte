<script lang="ts">
	import { format } from 'date-fns';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Project } from '$lib/types/project.type';
	import type { ImageUpdateInfoDto } from '$lib/types/image.type';
	import { ProjectsIcon, ImagesIcon } from '$lib/icons';

	type ProjectUpdateRow = {
		id: string;
		projectId: string;
		name: string;
		imageSummary: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		project: Project;
	};

	interface Props {
		projects: Paginated<Project>;
		requestOptions: SearchPaginationSortRequest;
		updateInfoByRef?: Record<string, ImageUpdateInfoDto>;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
	}

	let { projects = $bindable(), requestOptions = $bindable(), updateInfoByRef = {}, onRefreshData }: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});

	function formatCheckedAt(value: string) {
		if (!value) return '-';
		const parsed = new Date(value);
		if (Number.isNaN(parsed.getTime())) return '-';
		return format(parsed, 'PP p');
	}

	function summarizeImageRefs(imageRefs: string[]) {
		if (imageRefs.length === 0) return '-';
		if (imageRefs.length === 1) return imageRefs[0];
		return `${imageRefs[0]} +${imageRefs.length - 1} more`;
	}

	function resolveProjectValue(project: Project, mode: 'current' | 'latest') {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? [];
		if (updatedRefs.length === 0) return '-';
		if (updatedRefs.length > 1) {
			return m.images_has_updates();
		}

		const info = updateInfoByRef[updatedRefs[0]];
		if (!info) return '-';

		const digest = mode === 'current' ? info.currentDigest : info.latestDigest;
		if (digest?.trim()) return digest.trim();

		const version = mode === 'current' ? info.currentVersion : info.latestVersion;
		if (version?.trim()) return version.trim();

		return '-';
	}

	function resolveCheckedAt(project: Project) {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? [];
		if (updatedRefs.length === 1) {
			return updateInfoByRef[updatedRefs[0]]?.checkTime ?? project.updateInfo?.lastCheckedAt ?? '';
		}
		return project.updateInfo?.lastCheckedAt ?? '';
	}

	function mapProjectRow(project: Project): ProjectUpdateRow {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? project.updateInfo?.imageRefs ?? [];
		return {
			id: project.id,
			projectId: project.id,
			name: project.name,
			imageSummary: summarizeImageRefs(updatedRefs),
			currentValue: resolveProjectValue(project, 'current'),
			latestValue: resolveProjectValue(project, 'latest'),
			checkedAt: resolveCheckedAt(project),
			project
		};
	}

	const tableItems = $derived<Paginated<ProjectUpdateRow>>({
		...projects,
		data: (projects.data ?? []).map(mapProjectRow)
	});

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'imageSummary', title: m.common_image(), sortable: false, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.image_update_current_label(), sortable: false, cell: DigestCell },
		{ accessorKey: 'latestValue', title: m.image_update_latest_digest_label(), sortable: false, cell: DigestCell },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cell: CheckedAtCell }
	] satisfies ColumnSpec<ProjectUpdateRow>[];

	const mobileFields = [
		{ id: 'imageSummary', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.image_update_current_label(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_digest_label(), defaultVisible: true },
		{ id: 'checkedAt', label: m.common_updated(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item }: { item: ProjectUpdateRow })}
	<a class="font-medium hover:underline" href={`/projects/${item.projectId}`}>
		{item.name}
	</a>
{/snippet}

{#snippet ImageCell({ item }: { item: ProjectUpdateRow })}
	<div class="flex items-center gap-2">
		<ImagesIcon class="text-muted-foreground size-3.5 shrink-0" />
		<span class="truncate text-sm" title={item.imageSummary !== '-' ? item.imageSummary : undefined}>
			{item.imageSummary}
		</span>
	</div>
{/snippet}

{#snippet DigestCell({ value }: { value: unknown })}
	{@const text = typeof value === 'string' ? value : '-'}
	<span class="font-mono text-xs break-all whitespace-normal" title={text !== '-' ? text : undefined}>
		{text}
	</span>
{/snippet}

{#snippet CheckedAtCell({ value }: { value: unknown })}
	<span class="text-sm">{formatCheckedAt(typeof value === 'string' ? value : '')}</span>
{/snippet}

{#snippet ProjectUpdatesMobileCard({ item }: { item: ProjectUpdateRow })}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: ProjectsIcon,
			variant: 'amber' as const
		})}
		title={(item: ProjectUpdateRow) => item.name}
		subtitle={(item: ProjectUpdateRow) => item.imageSummary}
		fields={[
			{
				label: m.image_update_current_label(),
				getValue: (item: ProjectUpdateRow) => item.currentValue
			},
			{
				label: m.image_update_latest_digest_label(),
				getValue: (item: ProjectUpdateRow) => item.latestValue
			},
			{
				label: m.common_updated(),
				getValue: (item: ProjectUpdateRow) => formatCheckedAt(item.checkedAt)
			}
		]}
		onclick={(item: ProjectUpdateRow) => {
			window.location.href = `/projects/${item.projectId}`;
		}}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-project-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		await onRefreshData(options);
		return {
			...projects,
			data: (projects.data ?? []).map(mapProjectRow)
		};
	}}
	{columns}
	{mobileFields}
	mobileCard={ProjectUpdatesMobileCard}
	withoutFilters
	selectionDisabled
/>
