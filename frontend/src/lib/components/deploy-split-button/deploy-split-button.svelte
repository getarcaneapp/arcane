<script lang="ts">
	import { ArcaneButton, arcaneButtonVariants, type ArcaneButtonSize } from '#lib/components/arcane-button/index.js';
	import * as ButtonGroup from '#lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ArrowDownIcon, TerminalIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { cn } from '#lib/utils';
	import DeployOptionsMenuItems from './deploy-options-menu-items.svelte';

	let {
		size = 'default',
		showLabel = true,
		loading = false,
		customLabel,
		onDeploy,
		onDeployWatch
	}: {
		size?: ArcaneButtonSize;
		showLabel?: boolean;
		loading?: boolean;
		customLabel?: string;
		onDeploy: () => void | Promise<void>;
		onDeployWatch?: () => void | Promise<void>;
	} = $props();
</script>

<ButtonGroup.Root>
	<ArcaneButton action="deploy" {size} {showLabel} {loading} {customLabel} onclick={() => onDeploy?.()} />

	<DropdownMenu.Root>
		<DropdownMenu.Trigger
			class={cn(
				arcaneButtonVariants({ tone: 'outline-primary', size: 'icon' }),
				size === 'sm' && 'size-8 rounded-lg',
				size === 'lg' && 'size-10 rounded-xl'
			)}
			aria-label={m.common_open_menu()}
			disabled={loading}
			onclick={(event) => event.stopPropagation()}
			onpointerdown={(event) => event.stopPropagation()}
		>
			<ArrowDownIcon class="size-4" />
		</DropdownMenu.Trigger>

		<DropdownMenu.Content align="end" class="w-72">
			<DeployOptionsMenuItems />

			{#if onDeployWatch}
				<DropdownMenu.Separator />
				<DropdownMenu.Item onclick={() => onDeployWatch?.()}>
					<TerminalIcon class="size-4" />
					{m.watch_output()}
				</DropdownMenu.Item>
			{/if}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
</ButtonGroup.Root>
