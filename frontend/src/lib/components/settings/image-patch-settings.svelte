<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import type { MixedSettingValue, SettingKey } from '#lib/types/settings.js';

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

	let {
		formInputs = $bindable(),
		environmentId,
		mixed
	}: {
		formInputs: ImagePatchFormInputs;
		environmentId: string;
		mixed: (key: SettingKey) => MixedSettingValue[] | undefined;
	} = $props();
	const vulnerabilityManagementEnabled = $derived(featureStore.isEnabled('vulnerabilityManagement', environmentId));
</script>

<SettingsSection title={m.security_image_patching_heading()}>
	<SettingsRow
		for="imageAutoPatchEnabledSwitch"
		label={m.security_image_auto_patch_enabled_label()}
		description={m.security_image_auto_patch_enabled_description()}
		layout="switch"
		mixed={mixed('imageAutoPatchEnabled')}
	>
		<Switch
			id="imageAutoPatchEnabledSwitch"
			disabled={!vulnerabilityManagementEnabled}
			bind:checked={formInputs.imageAutoPatchEnabled.value}
			data-mixed={!!mixed('imageAutoPatchEnabled') || undefined}
		/>
	</SettingsRow>

	<SettingsRow
		for="imagePatchAllPlatformsSwitch"
		label={m.security_image_patch_all_platforms_label()}
		description={m.security_image_patch_all_platforms_description()}
		layout="switch"
		mixed={mixed('imagePatchAllPlatforms')}
	>
		<Switch
			id="imagePatchAllPlatformsSwitch"
			bind:checked={formInputs.imagePatchAllPlatforms.value}
			data-mixed={!!mixed('imagePatchAllPlatforms') || undefined}
		/>
	</SettingsRow>

	<SettingsRow
		for="imagePatchSuffix"
		label={m.security_image_patch_suffix_label()}
		description={m.security_image_patch_suffix_description()}
		error={formInputs.imagePatchSuffix.error}
		mixed={mixed('imagePatchSuffix')}
	>
		<Input
			id="imagePatchSuffix"
			bind:value={formInputs.imagePatchSuffix.value}
			placeholder={mixed('imagePatchSuffix') ? m.mixed() : 'patched'}
			aria-invalid={!!formInputs.imagePatchSuffix.error}
		/>
	</SettingsRow>

	<SettingsRow
		for="imagePatchTimeoutSec"
		label={m.security_image_patch_timeout_label()}
		description={m.security_image_patch_timeout_description()}
		error={formInputs.imagePatchTimeoutSec.error}
		mixed={mixed('imagePatchTimeoutSec')}
	>
		<Input
			id="imagePatchTimeoutSec"
			type="number"
			bind:value={formInputs.imagePatchTimeoutSec.value}
			placeholder={mixed('imagePatchTimeoutSec') ? m.mixed() : '600'}
			aria-invalid={!!formInputs.imagePatchTimeoutSec.error}
		/>
	</SettingsRow>
</SettingsSection>
