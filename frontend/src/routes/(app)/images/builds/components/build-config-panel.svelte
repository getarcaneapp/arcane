<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { ArrowDownIcon } from '$lib/icons';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import type { BuildFormInputsStore } from './build-form.types';

	let {
		inputs,
		showAdvanced = $bindable(false),
		onSubmit
	}: {
		inputs: BuildFormInputsStore;
		showAdvanced?: boolean;
		onSubmit?: () => void;
	} = $props();
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
						/>

						<FormInput
							label={m.build_args()}
							type="textarea"
							rows={3}
							placeholder={m.build_args_placeholder()}
							description={m.build_args_description()}
							bind:input={$inputs.buildArgs}
						/>
					</div>
				</Collapsible.Content>
			</Collapsible.Root>
		</div>
	</form>
</div>
