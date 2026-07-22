<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import PruneModeCard from '$lib/components/prune/prune-mode-card.svelte';
	import { m } from '$lib/paraglide/messages';
	import { DockerBrandIcon } from '$lib/icons';
	import type { DockerTabProps } from './tab-props';

	let { formInputs, shellSelectValue, handleShellSelectChange, shellOptions }: DockerTabProps = $props();

	const deployPullPolicyOptions = [
		{ value: 'missing', label: 'Missing', description: m.deploy_pull_policy_missing() },
		{ value: 'always', label: m.common_always(), description: m.deploy_pull_policy_always() },
		{ value: 'never', label: m.common_never(), description: m.deploy_pull_policy_never() }
	];

	const pruneContainerModes = [
		{ value: 'none', label: m.none() },
		{ value: 'stopped', label: m.prune_stopped_containers() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneImageModes = [
		{ value: 'none', label: m.none() },
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.all_unused() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneVolumeModes = [
		{ value: 'none', label: m.none() },
		{ value: 'anonymous', label: m.prune_volumes_mode_anonymous() },
		{ value: 'all', label: m.all_unused(), destructive: true }
	];
	const pruneNetworkModes = [
		{ value: 'none', label: m.none() },
		{ value: 'unused', label: m.unused_networks() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneBuildCacheModes = [
		{ value: 'none', label: m.none() },
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
					placeholder={m.select_shell_placeholder()}
					options={[...shellOptions, { value: 'custom', label: m.custom(), description: m.docker_shell_custom_description() }]}
				/>

				{#if shellSelectValue === 'custom'}
					<div class="pt-2">
						<TextInputWithLabel
							bind:value={$formInputs.defaultShell.value}
							error={$formInputs.defaultShell.error}
							label={m.custom()}
							placeholder={m.bin_sh_placeholder()}
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
		</div>

		<div class="border-t pt-6">
			<SettingsRow layout="inline" label={m.docker_auto_inject_env_label()} description={m.docker_auto_inject_env_description()}>
				<Switch id="auto-inject-env" bind:checked={$formInputs.autoInjectEnv.value} />
			</SettingsRow>
		</div>

		<div class="space-y-4 border-t pt-6">
			<div class="space-y-0.5">
				<h3 class="text-sm font-medium">{m.prune_options_title()}</h3>
				<p class="text-xs text-muted-foreground">{m.prune_options_description()}</p>
			</div>

			<div class="grid gap-2 sm:grid-cols-2">
				<PruneModeCard
					title={m.containers()}
					description={m.scheduled_prune_containers_description()}
					modeOptions={pruneContainerModes}
					bind:value={$formInputs.pruneContainerMode.value}
					bind:untilValue={$formInputs.pruneContainerUntil.value}
				/>
				<PruneModeCard
					title={m.images()}
					description={m.scheduled_prune_images_description()}
					modeOptions={pruneImageModes}
					bind:value={$formInputs.pruneImageMode.value}
					bind:untilValue={$formInputs.pruneImageUntil.value}
				/>
				<PruneModeCard
					title={m.resource_volumes_cap()}
					description={m.scheduled_prune_volumes_description()}
					modeOptions={pruneVolumeModes}
					bind:value={$formInputs.pruneVolumeMode.value}
					warningTitle={m.prune_volumes_warning_title()}
					warningDescription={m.scheduled_prune_volumes_warning()}
				/>
				<PruneModeCard
					title={m.resource_networks_cap()}
					description={m.scheduled_prune_networks_description()}
					modeOptions={pruneNetworkModes}
					bind:value={$formInputs.pruneNetworkMode.value}
					bind:untilValue={$formInputs.pruneNetworkUntil.value}
				/>
				<PruneModeCard
					title={m.build_cache()}
					description={m.scheduled_prune_build_cache_description()}
					modeOptions={pruneBuildCacheModes}
					bind:value={$formInputs.pruneBuildCacheMode.value}
					bind:untilValue={$formInputs.pruneBuildCacheUntil.value}
				/>
			</div>
		</div>
	</Card.Content>
</Card.Root>
