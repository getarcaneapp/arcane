<script lang="ts">
	import { ArcaneButton, arcaneButtonVariants, type ArcaneButtonSize } from '$lib/components/arcane-button/index.js';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArrowDownIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { deployOptionsStore, type DeployPullPolicy } from '$lib/stores/deploy-options.store.svelte';
	import { cn } from '$lib/utils';

	let {
		size = 'default',
		showLabel = true,
		loading = false,
		customLabel,
		onDeploy
	}: {
		size?: ArcaneButtonSize;
		showLabel?: boolean;
		loading?: boolean;
		customLabel?: string;
		onDeploy: () => void | Promise<void>;
	} = $props();

	function setPullPolicy(value: string) {
		deployOptionsStore.setPullPolicy(value as DeployPullPolicy);
	}

	function setForceRecreate(value: boolean) {
		deployOptionsStore.setForceRecreate(value);
	}
</script>

<ButtonGroup.Root>
	<ArcaneButton action="deploy" {size} {showLabel} {loading} {customLabel} onclick={() => onDeploy?.()} />

	<DropdownMenu.Root>
		<DropdownMenu.Trigger
			class={cn(
				arcaneButtonVariants({ tone: 'outline-primary', size: 'icon' }),
				size === 'sm' && 'size-8 rounded-md',
				size === 'lg' && 'size-10 rounded-md'
			)}
			aria-label={m.common_open_menu()}
			disabled={loading}
			onclick={(event) => event.stopPropagation()}
			onpointerdown={(event) => event.stopPropagation()}
		>
			<ArrowDownIcon class="size-4" />
		</DropdownMenu.Trigger>

		<DropdownMenu.Content align="end" class="w-72">
			<DropdownMenu.Label>{m.settings_default_deploy_pull_policy()}</DropdownMenu.Label>
			<DropdownMenu.RadioGroup value={deployOptionsStore.pullPolicy} onValueChange={setPullPolicy}>
				<DropdownMenu.RadioItem value="missing">
					<div class="flex flex-col gap-0.5">
						<span class="font-medium">Missing</span>
						<span class="text-muted-foreground text-xs">{m.deploy_pull_policy_missing()}</span>
					</div>
				</DropdownMenu.RadioItem>
				<DropdownMenu.RadioItem value="always">
					<div class="flex flex-col gap-0.5">
						<span class="font-medium">{m.common_always()}</span>
						<span class="text-muted-foreground text-xs">{m.deploy_pull_policy_always()}</span>
					</div>
				</DropdownMenu.RadioItem>
				<DropdownMenu.RadioItem value="never">
					<div class="flex flex-col gap-0.5">
						<span class="font-medium">{m.common_never()}</span>
						<span class="text-muted-foreground text-xs">{m.deploy_pull_policy_never()}</span>
					</div>
				</DropdownMenu.RadioItem>
			</DropdownMenu.RadioGroup>

			<DropdownMenu.Separator />

			<DropdownMenu.CheckboxItem
				checked={deployOptionsStore.forceRecreate}
				onCheckedChange={(checked) => setForceRecreate(checked === true)}
			>
				{m.deploy_force_recreate()}
			</DropdownMenu.CheckboxItem>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
</ButtonGroup.Root>
