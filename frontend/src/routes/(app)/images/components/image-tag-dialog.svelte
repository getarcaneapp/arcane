<script lang="ts">
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import RepoTagFields from '$lib/components/form/repo-tag-fields.svelte';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { imageService } from '$lib/services/image-service';
	import { m } from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	type Props = {
		open: boolean;
		imageId: string;
		defaultRepository?: string;
		onOpenChange?: (open: boolean) => void;
		onTagged?: () => Promise<void> | void;
	};

	let { open = $bindable(false), imageId, defaultRepository = '', onOpenChange, onTagged }: Props = $props();

	const schema = z.object({
		repository: z.string().min(1, m.images_tag_repository_required()),
		tag: z.string().optional().default('latest')
	});

	let formData = $derived({
		repository: defaultRepository || '',
		tag: 'latest'
	});

	let { inputs, ...form } = $derived(createForm<typeof schema>(schema, formData));
	let isTagging = $state(false);

	function handleOpenChange(nextOpen: boolean) {
		if (!nextOpen && isTagging) return;
		open = nextOpen;
		onOpenChange?.(nextOpen);
	}

	async function handleSubmit() {
		const data = form.validate();
		if (!data || isTagging) return;

		isTagging = true;
		try {
			await imageService.tagImage(imageId, {
				repository: data.repository.trim(),
				tag: data.tag?.trim() || undefined
			});
			toast.success(m.images_tag_success());
			await onTagged?.();
			handleOpenChange(false);
		} catch (error) {
			console.error('Failed to tag image:', error);
			toast.error(m.images_tag_failed());
		} finally {
			isTagging = false;
		}
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	title={m.images_tag_image()}
	description={m.images_tag_dialog_description()}
	contentClass="sm:max-w-[520px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
			<RepoTagFields bind:repository={$inputs.repository} bind:tag={$inputs.tag} />
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			onCancel={() => handleOpenChange(false)}
			submitAction="base"
			submitLabel={m.images_tag_image()}
			submitLoading={isTagging}
			submitDisabled={isTagging}
			onSubmit={handleSubmit}
		/>
	{/snippet}
</ResponsiveDialog.Root>
