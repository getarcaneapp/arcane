<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { ArrowDownIcon, SendEmailIcon } from '$lib/icons';

	export interface NotificationProviderTestOption {
		label: string;
		testType?: string;
	}

	interface Props {
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: (testType?: string) => void;
		options?: NotificationProviderTestOption[];
	}

	let {
		disabled = false,
		isTesting = false,
		onTest,
		options = [
			{ label: m.notifications_email_test_simple(), testType: 'simple' },
			{ label: m.notifications_email_test_image_update(), testType: 'image-update' },
			{ label: m.notifications_email_test_batch_image_update(), testType: 'batch-image-update' },
			{ label: m.notifications_test_vulnerability_notification(), testType: 'vulnerability-found' },
			{ label: m.notifications_test_prune_report_notification(), testType: 'prune-report' }
		]
	}: Props = $props();
</script>

{#if onTest}
	<div class="pt-2">
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				<ArcaneButton
					action="base"
					tone="outline"
					disabled={disabled || isTesting}
					loading={isTesting}
					icon={SendEmailIcon}
					customLabel={m.notifications_test_notification()}
				>
					<ArrowDownIcon class="ml-2 size-4" />
				</ArcaneButton>
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="start">
				{#each options as option (`${option.label}-${option.testType ?? 'default'}`)}
					<DropdownMenu.Item onclick={() => onTest(option.testType)}>
						<SendEmailIcon class="size-4" />
						{option.label}
					</DropdownMenu.Item>
				{/each}
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</div>
{/if}
