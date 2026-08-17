<script lang="ts">
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { m } from '#lib/paraglide/messages';
	import { deployOptionsStore, type DeployPullPolicy } from '#lib/stores/deploy-options.store.svelte';

	function setPullPolicy(value: string) {
		deployOptionsStore.setPullPolicy(value as DeployPullPolicy);
	}
</script>

<DropdownMenu.Label>{m.settings_default_deploy_pull_policy()}</DropdownMenu.Label>
<DropdownMenu.RadioGroup value={deployOptionsStore.pullPolicy} onValueChange={setPullPolicy}>
	<DropdownMenu.RadioItem value="missing">
		<div class="flex flex-col gap-0.5">
			<span class="font-medium">Missing</span>
			<span class="text-xs text-muted-foreground">{m.deploy_pull_policy_missing()}</span>
		</div>
	</DropdownMenu.RadioItem>
	<DropdownMenu.RadioItem value="always">
		<div class="flex flex-col gap-0.5">
			<span class="font-medium">{m.common_always()}</span>
			<span class="text-xs text-muted-foreground">{m.deploy_pull_policy_always()}</span>
		</div>
	</DropdownMenu.RadioItem>
	<DropdownMenu.RadioItem value="never">
		<div class="flex flex-col gap-0.5">
			<span class="font-medium">{m.common_never()}</span>
			<span class="text-xs text-muted-foreground">{m.deploy_pull_policy_never()}</span>
		</div>
	</DropdownMenu.RadioItem>
</DropdownMenu.RadioGroup>

<DropdownMenu.Separator />

<DropdownMenu.CheckboxItem
	checked={deployOptionsStore.forceRecreate}
	onCheckedChange={(checked) => deployOptionsStore.setForceRecreate(checked === true)}
>
	{m.deploy_force_recreate()}
</DropdownMenu.CheckboxItem>

<DropdownMenu.CheckboxItem
	checked={deployOptionsStore.removeOrphans}
	onCheckedChange={(checked) => deployOptionsStore.setRemoveOrphans(checked === true)}
>
	{m.deploy_remove_orphans()}
</DropdownMenu.CheckboxItem>

<DropdownMenu.CheckboxItem
	checked={deployOptionsStore.recreateVolumes}
	onCheckedChange={(checked) => deployOptionsStore.setRecreateVolumes(checked === true)}
>
	{m.deploy_recreate_volumes()}
</DropdownMenu.CheckboxItem>
