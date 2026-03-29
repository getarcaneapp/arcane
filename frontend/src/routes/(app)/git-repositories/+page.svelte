<script lang="ts">
	import GitRepositoriesPage from '../customize/git-repositories/+page.svelte';

	let { data } = $props();
</script>

<GitRepositoriesPage {data} />
