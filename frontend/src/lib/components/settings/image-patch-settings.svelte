<script lang="ts">
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { m } from '#lib/paraglide/messages.js';

	type ImagePatchFormValues = {
		imagePatchSuffix: string;
		imagePatchTimeoutSec: number;
		imagePatchAllPlatforms: boolean;
		imageAutoPatchEnabled: boolean;
	};

	type FormField<T> = {
		value: T;
		error: string | null;
	};

	type ImagePatchFormInputs = Record<string, FormField<unknown>> & {
		[K in keyof ImagePatchFormValues]: FormField<ImagePatchFormValues[K]>;
	};

	let { formInputs = $bindable(), environmentId }: { formInputs: ImagePatchFormInputs; environmentId?: string } = $props();
	const targetEnvironmentId = $derived(environmentId ?? environmentStore.selected?.id ?? '0');
	const vulnerabilityManagementEnabled = $derived(featureStore.isEnabled('vulnerabilityManagement', targetEnvironmentId));
</script>

<SettingsSection title={m.security_image_patching_heading()}>
	<SettingsRow
		for="imageAutoPatchEnabledSwitch"
		label={m.security_image_auto_patch_enabled_label()}
		description={m.security_image_auto_patch_enabled_description()}
		layout="switch"
	>
		<Switch
			id="imageAutoPatchEnabledSwitch"
			disabled={!vulnerabilityManagementEnabled}
			bind:checked={formInputs.imageAutoPatchEnabled.value}
		/>
	</SettingsRow>

	<SettingsRow
		for="imagePatchAllPlatformsSwitch"
		label={m.security_image_patch_all_platforms_label()}
		description={m.security_image_patch_all_platforms_description()}
		layout="switch"
	>
		<Switch id="imagePatchAllPlatformsSwitch" bind:checked={formInputs.imagePatchAllPlatforms.value} />
	</SettingsRow>

	<SettingsRow
		for="imagePatchSuffix"
		label={m.security_image_patch_suffix_label()}
		description={m.security_image_patch_suffix_description()}
		error={formInputs.imagePatchSuffix.error}
	>
		<Input
			id="imagePatchSuffix"
			bind:value={formInputs.imagePatchSuffix.value}
			placeholder="patched"
			aria-invalid={!!formInputs.imagePatchSuffix.error}
		/>
	</SettingsRow>

	<SettingsRow
		for="imagePatchTimeoutSec"
		label={m.security_image_patch_timeout_label()}
		description={m.security_image_patch_timeout_description()}
		error={formInputs.imagePatchTimeoutSec.error}
	>
		<Input
			id="imagePatchTimeoutSec"
			type="number"
			bind:value={formInputs.imagePatchTimeoutSec.value}
			placeholder="600"
			aria-invalid={!!formInputs.imagePatchTimeoutSec.error}
		/>
	</SettingsRow>
</SettingsSection>
