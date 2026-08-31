<script lang="ts">
	import { Switch } from '#lib/components/ui/switch/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { SecurityIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import type { Readable } from 'svelte/store';
	import SectionCard from '#lib/components/section-card.svelte';

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

	type ImagePatchFormInputs = Readable<
		Record<string, FormField<unknown>> & {
			[K in keyof ImagePatchFormValues]: FormField<ImagePatchFormValues[K]>;
		}
	>;

	let { formInputs }: { formInputs: ImagePatchFormInputs } = $props();
</script>

<SectionCard
	title={m.security_image_patching_heading()}
	icon={SecurityIcon}
	class="flex flex-col"
	contentClass="divide-y divide-border/40 lg:p-6 lg:pt-0 [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0"
>
	<SettingsRow
		label={m.security_image_auto_patch_enabled_label()}
		description={m.security_image_auto_patch_enabled_description()}
		layout="inline"
	>
		<Switch id="imageAutoPatchEnabledSwitch" bind:checked={$formInputs.imageAutoPatchEnabled.value} />
	</SettingsRow>

	<SettingsRow
		label={m.security_image_patch_all_platforms_label()}
		description={m.security_image_patch_all_platforms_description()}
		layout="inline"
	>
		<Switch id="imagePatchAllPlatformsSwitch" bind:checked={$formInputs.imagePatchAllPlatforms.value} />
	</SettingsRow>

	<div class="max-w-xl">
		<TextInputWithLabel
			bind:value={$formInputs.imagePatchSuffix.value}
			error={$formInputs.imagePatchSuffix.error}
			label={m.security_image_patch_suffix_label()}
			description={m.security_image_patch_suffix_description()}
			placeholder="patched"
			type="text"
		/>
	</div>

	<div class="max-w-xs">
		<TextInputWithLabel
			bind:value={$formInputs.imagePatchTimeoutSec.value}
			error={$formInputs.imagePatchTimeoutSec.error}
			label={m.security_image_patch_timeout_label()}
			description={m.security_image_patch_timeout_description()}
			placeholder="600"
			type="number"
		/>
	</div>
</SectionCard>
