<script lang="ts">
	import * as DropdownMenu from '@arcane/ui/dropdown-menu';
	import { Spinner } from '@arcane/ui/spinner';

	import type { IconType } from '#lib/icons/index.js';

	interface Props {
		icon: IconType;
		label: string;
		onclick: () => void;
		loading?: boolean;
		disabled?: boolean;
		destructive?: boolean;
		title?: string;
	}

	let { icon: Icon, label, onclick, loading = false, disabled = false, destructive = false, title }: Props = $props();
</script>

<DropdownMenu.Item {onclick} disabled={disabled || loading} {title} variant={destructive ? 'destructive' : 'default'}>
	{#if loading}
		<Spinner class="size-4" />
	{:else}
		<Icon class="size-4" />
	{/if}
	{label}
</DropdownMenu.Item>
