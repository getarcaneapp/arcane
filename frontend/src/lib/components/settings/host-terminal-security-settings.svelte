<script lang="ts">
	import * as Alert from '#lib/components/ui/alert';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { AlertIcon, TerminalIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import type { Readable } from 'svelte/store';

	type HostTerminalSecurityFormValues = {
		hostTerminalEnabled: boolean;
		hostTerminalImage: string;
	};

	type FormField<T> = {
		value: T;
		error: string | null;
	};

	type HostTerminalSecurityFormInputs = Readable<
		Record<string, FormField<unknown>> & {
			[K in keyof HostTerminalSecurityFormValues]: FormField<HostTerminalSecurityFormValues[K]>;
		}
	>;

	let { formInputs }: { formInputs: HostTerminalSecurityFormInputs } = $props();
</script>

<Card.Root class="flex flex-col">
	<Card.Header icon={TerminalIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.security_host_terminal_heading()}</h2>
			</Card.Title>
		</div>
	</Card.Header>
	<Card.Content class="divide-y divide-border/40 lg:p-6 lg:pt-0 [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
		<SettingsRow
			label={m.security_host_terminal_enabled_label()}
			description={m.security_host_terminal_enabled_description()}
			layout="inline"
		>
			<Switch id="hostTerminalEnabledSwitch" bind:checked={$formInputs.hostTerminalEnabled.value} />
		</SettingsRow>

		<div class="max-w-xl">
			<TextInputWithLabel
				bind:value={$formInputs.hostTerminalImage.value}
				error={$formInputs.hostTerminalImage.error}
				label={m.security_host_terminal_image_label()}
				description={m.security_host_terminal_image_description()}
				placeholder="ghcr.io/getarcaneapp/tools:latest"
				type="text"
			/>
		</div>

		<div>
			<Alert.Root variant="destructive" class="py-2 [&>svg]:top-2">
				<AlertIcon class="size-4" />
				<Alert.Description class="text-xs">
					{m.security_host_terminal_note()}
				</Alert.Description>
			</Alert.Root>
		</div>
	</Card.Content>
</Card.Root>
