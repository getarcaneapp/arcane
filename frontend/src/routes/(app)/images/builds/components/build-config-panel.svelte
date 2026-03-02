<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { ArrowDownIcon } from '$lib/icons';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import type { BuildFormInputsStore } from './build-form.types';

	let {
		inputs,
		provider,
		showAdvanced = $bindable(false),
		onSubmit
	}: {
		inputs: BuildFormInputsStore;
		provider: 'local' | 'depot';
		showAdvanced?: boolean;
		onSubmit?: () => void;
	} = $props();

	const providerIsLocal = $derived(provider === 'local');
	const providerIsDepot = $derived(provider === 'depot');
</script>

<div class="space-y-7 p-8">
	<form onsubmit={preventDefault(() => onSubmit?.())} class="space-y-7">
		<div class="space-y-4">
			<FormInput
				label={m.image_tags()}
				type="text"
				placeholder={m.image_tags_placeholder()}
				description={m.image_tags_description()}
				bind:input={$inputs.tags}
			/>

			<Collapsible.Root bind:open={showAdvanced}>
				<Collapsible.Trigger
					class="text-muted-foreground hover:text-foreground hover:bg-accent flex w-full items-center justify-between rounded-md px-2 py-1.5 text-xs transition-colors"
				>
					{m.tabs_advanced()}
					<ArrowDownIcon class={showAdvanced ? 'size-4 rotate-180 transition-transform' : 'size-4 transition-transform'} />
				</Collapsible.Trigger>
				<Collapsible.Content>
					<div class="mt-4 grid gap-6">
						<!-- Advanced build options -->
						<div class="grid gap-4 sm:grid-cols-2">
							<FormInput
								label={m.dockerfile()}
								type="text"
								placeholder={m.dockerfile()}
								description={m.dockerfile_description()}
								bind:input={$inputs.dockerfile}
							/>

							<FormInput
								label={m.target_label()}
								type="text"
								placeholder={m.target_placeholder()}
								description={m.target_description()}
								bind:input={$inputs.target}
							/>
						</div>

						<FormInput
							label={m.platforms_label()}
							type="text"
							placeholder={m.platforms_placeholder()}
							description={m.platforms_description()}
							bind:input={$inputs.platforms}
							warningText={providerIsLocal ? m.build_provider_warning_single_platform_local() : undefined}
						/>

						<FormInput
							label={m.build_args()}
							type="textarea"
							rows={3}
							placeholder={m.build_args_placeholder()}
							description={m.build_args_description()}
							bind:input={$inputs.buildArgs}
						/>

						<FormInput
							label={m.common_labels()}
							type="textarea"
							rows={3}
							placeholder={m.build_labels_placeholder()}
							description={m.build_labels_description()}
							bind:input={$inputs.labels}
						/>

						<FormInput
							label={m.build_cache_from_label()}
							type="textarea"
							rows={2}
							placeholder={m.build_cache_from_placeholder()}
							description={m.build_cache_from_description()}
							bind:input={$inputs.cacheFrom}
						/>

						<FormInput
							label={m.build_cache_to_label()}
							type="textarea"
							rows={2}
							placeholder={m.build_cache_to_placeholder()}
							description={m.build_cache_to_description()}
							bind:input={$inputs.cacheTo}
							disabled={providerIsLocal}
							warningText={providerIsLocal ? m.build_provider_warning_unsupported_local() : undefined}
						/>

						<div class="grid gap-4 sm:grid-cols-2">
							<FormInput
								label={m.build_network_label()}
								type="text"
								placeholder={m.build_network_placeholder()}
								description={m.build_network_description()}
								bind:input={$inputs.network}
								disabled={providerIsDepot}
								warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
							/>

							<FormInput
								label={m.build_isolation_label()}
								type="text"
								placeholder={m.build_isolation_placeholder()}
								description={m.build_isolation_description()}
								bind:input={$inputs.isolation}
								disabled={providerIsDepot}
								warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
							/>
						</div>

						<div class="grid gap-4 sm:grid-cols-2">
							<FormInput
								label={m.build_shm_size_label()}
								type="text"
								placeholder={m.build_shm_size_placeholder()}
								description={m.build_shm_size_description()}
								bind:input={$inputs.shmSize}
								disabled={providerIsDepot}
								warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
							/>

							<FormInput
								label={m.build_entitlements_label()}
								type="textarea"
								rows={2}
								placeholder={m.build_entitlements_placeholder()}
								description={m.build_entitlements_description()}
								bind:input={$inputs.entitlements}
								disabled={providerIsLocal}
								warningText={providerIsLocal ? m.build_provider_warning_unsupported_local() : undefined}
							/>
						</div>

						<FormInput
							label={m.build_ulimits_label()}
							type="textarea"
							rows={2}
							placeholder={m.build_ulimits_placeholder()}
							description={m.build_ulimits_description()}
							bind:input={$inputs.ulimits}
							disabled={providerIsDepot}
							warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
						/>

						<FormInput
							label={m.build_extra_hosts_label()}
							type="textarea"
							rows={2}
							placeholder={m.build_extra_hosts_placeholder()}
							description={m.build_extra_hosts_description()}
							bind:input={$inputs.extraHosts}
							disabled={providerIsDepot}
							warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
						/>

						<div class="grid gap-4 sm:grid-cols-2">
							<FormInput
								label={m.build_privileged_label()}
								type="switch"
								description={m.build_privileged_description()}
								bind:input={$inputs.privileged}
								disabled={providerIsLocal}
								warningText={providerIsLocal ? m.build_provider_warning_unsupported_local() : undefined}
							/>

							<FormInput
								label={m.build_no_cache_label()}
								type="switch"
								description={m.build_no_cache_description()}
								bind:input={$inputs.noCache}
							/>
						</div>

						<FormInput
							label={m.build_pull_base_images_label()}
							type="switch"
							description={m.build_pull_base_images_description()}
							bind:input={$inputs.pull}
						/>
					</div>
				</Collapsible.Content>
			</Collapsible.Root>
		</div>
	</form>
</div>
