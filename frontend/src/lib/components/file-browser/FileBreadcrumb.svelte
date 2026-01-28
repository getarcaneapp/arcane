<script lang="ts">
	import * as Breadcrumb from '$lib/components/ui/breadcrumb';
	import { ArrowRightIcon, DashboardIcon } from '$lib/icons';

	let {
		path,
		rootLabel,
		onNavigate
	}: {
		path: string;
		rootLabel?: string;
		onNavigate: (path: string) => void;
	} = $props();

	let segments = $derived.by(() => {
		const parts = path.split('/').filter((p) => p);
		return parts.map((name, index) => ({
			name,
			path: '/' + parts.slice(0, index + 1).join('/')
		}));
	});
</script>

<Breadcrumb.Root>
	<Breadcrumb.List>
		<Breadcrumb.Item>
			<Breadcrumb.Link onclick={() => onNavigate('/')} class="cursor-pointer">
				<DashboardIcon class="h-4 w-4" />
			</Breadcrumb.Link>
		</Breadcrumb.Item>

		{#if rootLabel}
			<Breadcrumb.Separator>
				<ArrowRightIcon class="h-4 w-4" />
			</Breadcrumb.Separator>
			<Breadcrumb.Item>
				{#if segments.length === 0}
					<Breadcrumb.Page>{rootLabel}</Breadcrumb.Page>
				{:else}
					<Breadcrumb.Link onclick={() => onNavigate('/')} class="cursor-pointer">
						{rootLabel}
					</Breadcrumb.Link>
				{/if}
			</Breadcrumb.Item>
		{/if}

		{#each segments as segment, i}
			<Breadcrumb.Separator>
				<ArrowRightIcon class="h-4 w-4" />
			</Breadcrumb.Separator>
			<Breadcrumb.Item>
				{#if i === segments.length - 1}
					<Breadcrumb.Page>{segment.name}</Breadcrumb.Page>
				{:else}
					<Breadcrumb.Link onclick={() => onNavigate(segment.path)} class="cursor-pointer">
						{segment.name}
					</Breadcrumb.Link>
				{/if}
			</Breadcrumb.Item>
		{/each}
	</Breadcrumb.List>
</Breadcrumb.Root>
