<script lang="ts">
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	type LifecycleSecurityFormValues = {
		lifecycleEnabled: boolean;
		lifecycleDefaultRunnerImage: string;
		lifecycleMaxTimeoutSec: number;
	};

	type FormField<T> = {
		value: T;
		error: string | null;
	};

	type LifecycleSecurityFormInputs = Record<string, FormField<unknown>> & {
		[K in keyof LifecycleSecurityFormValues]: FormField<LifecycleSecurityFormValues[K]>;
	};

	let { formInputs = $bindable() }: { formInputs: LifecycleSecurityFormInputs } = $props();
</script>

<SettingsSection title={m.security_lifecycle_hooks_heading()}>
	{#snippet actions()}
		<Alert.Root variant="warning" size="sm" class="max-w-xl">
			<AlertIcon class="size-4" />
			<Alert.Description>{m.security_lifecycle_hooks_note()}</Alert.Description>
		</Alert.Root>
	{/snippet}

	<SettingsRow
		for="lifecycleEnabledSwitch"
		label={m.security_lifecycle_enabled_label()}
		description={m.security_lifecycle_enabled_description()}
		layout="switch"
	>
		<Switch id="lifecycleEnabledSwitch" bind:checked={formInputs.lifecycleEnabled.value} />
	</SettingsRow>

	<SettingsRow
		for="lifecycleDefaultRunnerImage"
		label={m.security_lifecycle_runner_image_label()}
		description={m.security_lifecycle_runner_image_description()}
		helpText={m.security_lifecycle_runner_image_help()}
		error={formInputs.lifecycleDefaultRunnerImage.error}
	>
		<Input
			id="lifecycleDefaultRunnerImage"
			bind:value={formInputs.lifecycleDefaultRunnerImage.value}
			placeholder="alpine:latest"
			aria-invalid={!!formInputs.lifecycleDefaultRunnerImage.error}
		/>
	</SettingsRow>

	<SettingsRow
		for="lifecycleMaxTimeoutSec"
		label={m.security_lifecycle_max_timeout_label()}
		description={m.security_lifecycle_max_timeout_description()}
		helpText={m.security_lifecycle_max_timeout_help()}
		error={formInputs.lifecycleMaxTimeoutSec.error}
	>
		<Input
			id="lifecycleMaxTimeoutSec"
			type="number"
			bind:value={formInputs.lifecycleMaxTimeoutSec.value}
			aria-invalid={!!formInputs.lifecycleMaxTimeoutSec.error}
		/>
	</SettingsRow>
</SettingsSection>
