<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import PruneModeCard from '$lib/components/prune/prune-mode-card.svelte';
	import { m } from '$lib/paraglide/messages';
	import { DockerBrandIcon } from '$lib/icons';

	let { formInputs, shellSelectValue, handleShellSelectChange, shellOptions } = $props();

	const deployPullPolicyOptions = [
		{ value: 'missing', label: 'Missing', description: m.deploy_pull_policy_missing() },
		{ value: 'always', label: m.common_always(), description: m.deploy_pull_policy_always() },
		{ value: 'never', label: m.common_never(), description: m.deploy_pull_policy_never() }
	];

	const pruneContainerModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'stopped', label: m.prune_stopped_containers() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneImageModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.prune_images_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneVolumeModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'anonymous', label: m.prune_volumes_mode_anonymous() },
		{ value: 'all', label: m.prune_volumes_mode_all(), destructive: true }
	];
	const pruneNetworkModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'unused', label: m.prune_unused_networks() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneBuildCacheModes = [
		{ value: 'none', label: m.prune_mode_none() },
		{ value: 'unused', label: m.prune_build_cache_mode_unused() },
		{ value: 'all', label: m.prune_build_cache_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
</script>

<Card.Root class="flex flex-col">
	<Card.Header icon={DockerBrandIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.environments_docker_settings_title()}</h2>
			</Card.Title>
			<Card.Description>{m.environments_config_description()}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="space-y-6 p-4">
		<div class="grid gap-6 sm:grid-cols-2">
			<div class="space-y-2">
				<SelectWithLabel
					id="shellSelectValue"
					name="shellSelectValue"
					value={shellSelectValue}
					onValueChange={handleShellSelectChange}
					label={m.docker_default_shell_label()}
					description={m.docker_default_shell_description()}
					placeholder={m.docker_default_shell_placeholder()}
					options={[...shellOptions, { value: 'custom', label: m.custom(), description: m.docker_shell_custom_description() }]}
				/>

				{#if shellSelectValue === 'custom'}
					<div class="pt-2">
						<TextInputWithLabel
							bind:value={$formInputs.defaultShell.value}
							error={$formInputs.defaultShell.error}
							label={m.custom()}
							placeholder={m.docker_shell_custom_path_placeholder()}
							helpText={m.docker_shell_custom_path_help()}
							type="text"
						/>
					</div>
				{/if}
			</div>

			<div class="space-y-2">
				<SelectWithLabel
					id="defaultDeployPullPolicy"
					name="defaultDeployPullPolicy"
					bind:value={$formInputs.defaultDeployPullPolicy.value}
					label={m.settings_default_deploy_pull_policy()}
					description={m.settings_default_deploy_pull_policy_description()}
					options={deployPullPolicyOptions}
					onValueChange={(v) => ($formInputs.defaultDeployPullPolicy.value = v as 'missing' | 'always' | 'never')}
				/>
			</div>

			<div class="space-y-4 rounded-lg border p-4">
				<div class="flex items-center justify-between">
					<div class="space-y-0.5">
						<Label for="auto-inject-env" class="text-sm font-medium">{m.docker_auto_inject_env_label()}</Label>
						<div class="text-muted-foreground text-xs">{m.docker_auto_inject_env_description()}</div>
					</div>
					<Switch id="auto-inject-env" bind:checked={$formInputs.autoInjectEnv.value} />
				</div>
			</div>
		</div>

		<div class="space-y-4 rounded-lg border p-4">
			<div class="space-y-0.5">
				<h3 class="text-sm font-medium">{m.prune_options_title()}</h3>
				<p class="text-muted-foreground text-xs">{m.prune_options_description()}</p>
			</div>

			<div class="grid gap-2 sm:grid-cols-2">
				<PruneModeCard
					title={m.prune_containers_label()}
					description={m.scheduled_prune_containers_description()}
					modeOptions={pruneContainerModes}
					bind:value={$formInputs.pruneContainerMode.value}
					bind:untilValue={$formInputs.pruneContainerUntil.value}
				/>
				<PruneModeCard
					title={m.prune_images_label()}
					description={m.scheduled_prune_images_description()}
					modeOptions={pruneImageModes}
					bind:value={$formInputs.pruneImageMode.value}
					bind:untilValue={$formInputs.pruneImageUntil.value}
				/>
				<PruneModeCard
					title={m.prune_volumes_label()}
					description={m.scheduled_prune_volumes_description()}
					modeOptions={pruneVolumeModes}
					bind:value={$formInputs.pruneVolumeMode.value}
					warningTitle={m.prune_volumes_warning_title()}
					warningDescription={m.scheduled_prune_volumes_warning()}
				/>
				<PruneModeCard
					title={m.prune_networks_label()}
					description={m.scheduled_prune_networks_description()}
					modeOptions={pruneNetworkModes}
					bind:value={$formInputs.pruneNetworkMode.value}
					bind:untilValue={$formInputs.pruneNetworkUntil.value}
				/>
				<PruneModeCard
					title={m.prune_build_cache_label()}
					description={m.scheduled_prune_build_cache_description()}
					modeOptions={pruneBuildCacheModes}
					bind:value={$formInputs.pruneBuildCacheMode.value}
					bind:untilValue={$formInputs.pruneBuildCacheUntil.value}
				/>
			</div>
		</div>
	</Card.Content>
</Card.Root>
