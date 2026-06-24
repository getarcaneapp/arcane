<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import { AlertIcon, CodeIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { Readable } from 'svelte/store';

	type LifecycleSecurityFormValues = {
		lifecycleEnabled: boolean;
		lifecycleDefaultRunnerImage: string;
		lifecycleMaxTimeoutSec: number;
	};

	type FormField<T> = {
		value: T;
		error: string | null;
	};

	type LifecycleSecurityFormInputs = Readable<
		Record<string, FormField<unknown>> & {
			[K in keyof LifecycleSecurityFormValues]: FormField<LifecycleSecurityFormValues[K]>;
		}
	>;

	let { formInputs }: { formInputs: LifecycleSecurityFormInputs } = $props();
</script>

<Card.Root class="flex flex-col">
	<Card.Header icon={CodeIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.security_lifecycle_hooks_heading()}</h2>
			</Card.Title>
		</div>
	</Card.Header>
	<Card.Content class="divide-border/40 divide-y lg:p-6 lg:pt-0 [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
		<SettingsRow
			label={m.security_lifecycle_enabled_label()}
			description={m.security_lifecycle_enabled_description()}
			layout="inline"
		>
			<Switch id="lifecycleEnabledSwitch" bind:checked={$formInputs.lifecycleEnabled.value} />
		</SettingsRow>

		<div class="max-w-xl">
			<TextInputWithLabel
				bind:value={$formInputs.lifecycleDefaultRunnerImage.value}
				error={$formInputs.lifecycleDefaultRunnerImage.error}
				label={m.security_lifecycle_runner_image_label()}
				description={m.security_lifecycle_runner_image_description()}
				helpText={m.security_lifecycle_runner_image_help()}
				placeholder="alpine:latest"
				type="text"
			/>
		</div>

		<div class="max-w-xs">
			<TextInputWithLabel
				bind:value={$formInputs.lifecycleMaxTimeoutSec.value}
				error={$formInputs.lifecycleMaxTimeoutSec.error}
				label={m.security_lifecycle_max_timeout_label()}
				description={m.security_lifecycle_max_timeout_description()}
				helpText={m.security_lifecycle_max_timeout_help()}
				type="number"
			/>
		</div>

		<div>
			<Alert.Root variant="warning" class="py-2 [&>svg]:top-2">
				<AlertIcon class="size-4" />
				<Alert.Description class="text-xs">
					{m.security_lifecycle_hooks_note()}
				</Alert.Description>
			</Alert.Root>
		</div>
	</Card.Content>
</Card.Root>
