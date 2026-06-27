<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog';
	import * as Select from '$lib/components/ui/select';
	import { Label } from '$lib/components/ui/label';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { containerService } from '$lib/services/container-service';
	import { handleApiResultWithCallbacks, tryCatch } from '$lib/utils/api';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	type Props = {
		containerId: string;
		containerName: string;
		onClose?: () => void;
		onComplete?: () => void | Promise<void>;
	};

	let { containerId, containerName, onClose, onComplete }: Props = $props();

	const SIGNALS = ['SIGKILL', 'SIGTERM', 'SIGINT', 'SIGHUP', 'SIGQUIT'];
	let open = $state(true);
	let signal = $state('SIGKILL');
	let isSubmitting = $state(false);

	async function handleConfirm() {
		if (!containerId) return;
		isSubmitting = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(containerService.killContainer(containerId, signal)),
			message: m.containers_kill_failed({ name: containerName }),
			setLoadingState: (value) => {
				isSubmitting = value;
			},
			async onSuccess(data) {
				toast.success(m.containers_kill_success({ name: containerName }), activityToastOptions(extractActivityId(data)));
				open = false;
				await onComplete?.();
			}
		});
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={(value) => {
		if (!value) onClose?.();
	}}
	title={m.containers_kill_title({ name: containerName })}
	description={m.containers_kill_description()}
>
	<div class="space-y-2 px-6 py-4">
		<Label for="kill-signal">{m.containers_kill_signal_label()}</Label>
		<Select.Root type="single" bind:value={signal}>
			<Select.Trigger id="kill-signal" class="w-full">
				<span class="truncate">{signal}</span>
			</Select.Trigger>
			<Select.Content>
				{#each SIGNALS as sig (sig)}
					<Select.Item value={sig}>{sig}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</div>

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 px-6 pb-6 sm:flex-row sm:justify-end">
			<ArcaneButton
				action="base"
				tone="outline"
				customLabel={m.common_cancel()}
				onclick={() => (open = false)}
				disabled={isSubmitting}
			/>
			<ArcaneButton
				action="base"
				tone="outline-destructive"
				customLabel={m.containers_kill_confirm()}
				onclick={handleConfirm}
				loading={isSubmitting}
				disabled={isSubmitting}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
