<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import { ContainersIcon, ProjectsIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';

	let { value }: { value: 'projects' | 'containers' } = $props();

	const environmentId = $derived(environmentStore.selected?.id ?? '0');
	const items: TabItem[] = $derived.by(() => {
		const visible: TabItem[] = [];
		if (hasPermission('projects:list', environmentId) || hasPermission('projects:read', environmentId)) {
			visible.push({ value: 'projects', label: m.projects_title(), icon: ProjectsIcon });
		}
		if (hasPermission('containers:list', environmentId) || hasPermission('containers:read', environmentId)) {
			visible.push({ value: 'containers', label: m.containers_title(), icon: ContainersIcon });
		}
		return visible;
	});

	function selectTab(next: string) {
		if (next === value) return;
		goto(`/workloads/${next}${page.url.search}`);
	}
</script>

<Tabs.Root {value}>
	<TabBar {items} {value} onValueChange={selectTab} />
</Tabs.Root>
