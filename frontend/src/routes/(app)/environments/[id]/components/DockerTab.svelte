<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { DockerBrandIcon } from '$lib/icons';

	let { formInputs, shellSelectValue, handleShellSelectChange, shellOptions, pruneModeDescription, pruneModeOptions } = $props();

	const upPullPolicyOptions = [
		{
			value: 'missing',
			label: 'Only pull missing images',
			description: m.projects_up_options_defaults_help()
		},
		{
			value: 'always',
			label: m.projects_up_option_pull_latest(),
			description: 'Pull the latest image tags before bringing the project up.'
		}
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
			<!-- Prune Mode -->
			<div class="space-y-2">
				<SelectWithLabel
					id="dockerPruneMode"
					name="pruneMode"
					bind:value={$formInputs.dockerPruneMode.value}
					label={m.docker_prune_action_label()}
					description={pruneModeDescription}
					placeholder={m.docker_prune_placeholder()}
					options={pruneModeOptions}
					onValueChange={(v) => ($formInputs.dockerPruneMode.value = v as 'all' | 'dangling')}
				/>
			</div>

			<!-- Default Shell -->
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

			<div class="space-y-4 rounded-lg border p-4">
				<div class="flex items-center justify-between">
					<div class="space-y-0.5">
						<Label for="auto-inject-env" class="text-sm font-medium">{m.docker_auto_inject_env_label()}</Label>
						<div class="text-muted-foreground text-xs">{m.docker_auto_inject_env_description()}</div>
					</div>
					<Switch id="auto-inject-env" bind:checked={$formInputs.autoInjectEnv.value} />
				</div>
			</div>

			<div class="space-y-4 rounded-lg border p-4 sm:col-span-2">
				<div class="space-y-0.5">
					<Label class="text-sm font-medium">{m.projects_up_options_title()}</Label>
					<div class="text-muted-foreground text-xs">
						Default behavior for project Up in this environment. Right-click the Up button to run one-time overrides.
					</div>
				</div>

				<div class="grid gap-4 sm:grid-cols-2">
					<div class="space-y-2">
						<SelectWithLabel
							id="projectUpDefaultPullPolicy"
							name="projectUpDefaultPullPolicy"
							bind:value={$formInputs.projectUpDefaultPullPolicy.value}
							label="Default Pull Policy"
							description="Choose how images are pulled when using Up by default."
							options={upPullPolicyOptions}
							onValueChange={(v) => ($formInputs.projectUpDefaultPullPolicy.value = v as 'missing' | 'always')}
						/>
					</div>

					<div class="flex items-center justify-between rounded-lg border p-3">
						<div class="space-y-0.5">
							<Label for="project-up-default-force-recreate" class="text-sm font-medium">
								{m.projects_up_option_recreate_containers()}
							</Label>
							<div class="text-muted-foreground text-xs">Enable to force container recreation on default Up actions.</div>
						</div>
						<Switch id="project-up-default-force-recreate" bind:checked={$formInputs.projectUpDefaultForceRecreate.value} />
					</div>
				</div>
			</div>
		</div>
	</Card.Content>
</Card.Root>
