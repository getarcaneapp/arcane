<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { Component } from 'svelte';

	interface Props {
		icon: Component<any>;
		label: string;
		onclick: () => void;
		loading?: boolean;
		disabled?: boolean;
		destructive?: boolean;
	}

	let { icon: Icon, label, onclick, loading = false, disabled = false, destructive = false }: Props = $props();
</script>

<DropdownMenu.Item {onclick} {disabled} variant={destructive ? 'destructive' : 'default'}>
	{#if loading}
		<Spinner class="size-4" />
	{:else}
		<Icon class="size-4" />
	{/if}
	{label}
</DropdownMenu.Item>
