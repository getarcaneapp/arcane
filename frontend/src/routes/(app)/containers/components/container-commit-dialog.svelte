<script lang="ts">
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import RepoTagFields from '$lib/components/form/repo-tag-fields.svelte';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { containerService } from '$lib/services/container-service';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';
	import { InfoIcon } from '$lib/icons';

	type Props = {
		open: boolean;
		containerId: string;
		containerName: string;
		onOpenChange?: (open: boolean) => void;
		onCommitted?: () => Promise<void> | void;
	};

	let { open = $bindable(false), containerId, containerName, onOpenChange, onCommitted }: Props = $props();

	const schema = z.object({
		repository: z.string().optional().default(''),
		tag: z.string().optional().default('latest'),
		comment: z.string().optional().default(''),
		author: z.string().optional().default(''),
		noPause: z.boolean().optional().default(false)
	});

	let formData = $derived({
		repository: '',
		tag: 'latest',
		comment: '',
		author: '',
		noPause: false
	});

	let { inputs, ...form } = $derived(createForm<typeof schema>(schema, formData));
	let isCommitting = $state(false);

	function handleOpenChange(nextOpen: boolean) {
		if (!nextOpen && isCommitting) return;
		open = nextOpen;
		onOpenChange?.(nextOpen);
	}

	async function handleSubmit() {
		const data = form.validate();
		if (!data || isCommitting) return;

		isCommitting = true;
		let didCommit = false;
		try {
			const result = await containerService.commitContainer(containerId, {
				repository: data.repository.trim() || undefined,
				tag: data.tag?.trim() || undefined,
				comment: data.comment.trim() || undefined,
				author: data.author.trim() || undefined,
				noPause: data.noPause
			});
			toast.success(m.containers_commit_success({ imageId: result.id }));
			didCommit = true;
			open = false;
			onOpenChange?.(false);
		} catch (error) {
			console.error('Failed to commit container:', error);
			toast.error(m.containers_commit_failed({ name: containerName }));
		} finally {
			isCommitting = false;
		}

		if (didCommit) {
			await onCommitted?.();
		}
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	title={m.containers_commit_title({ name: containerName })}
	description={m.containers_commit_description()}
	contentClass="sm:max-w-[560px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
			<Alert.Root class="border-cyan-500/30 bg-cyan-500/10 text-cyan-950 dark:text-cyan-100">
				<InfoIcon class="size-4" />
				<Alert.Description class="text-sm">{m.containers_commit_registry_note()}</Alert.Description>
			</Alert.Root>
			<RepoTagFields bind:repository={$inputs.repository} bind:tag={$inputs.tag} />
			<FormInput
				label={m.common_description()}
				placeholder={m.containers_commit_comment_placeholder()}
				bind:input={$inputs.comment}
			/>
			<FormInput label={m.common_author()} placeholder={m.arcane_placeholder()} bind:input={$inputs.author} />
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={$inputs.noPause.value} class="size-4 accent-primary" />
				<span>{m.containers_commit_no_pause()}</span>
			</label>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			onCancel={() => handleOpenChange(false)}
			submitAction="commit"
			submitTone="outline-primary"
			submitLoading={isCommitting}
			submitDisabled={isCommitting}
			onSubmit={handleSubmit}
		/>
	{/snippet}
</ResponsiveDialog.Root>
