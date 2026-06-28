<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';

	type ImagePullFormProps = {
		open: boolean;
		onPullFinished?: (success: boolean, imageName?: string, error?: string) => void;
	};

	let { open = $bindable(false), onPullFinished = () => {} }: ImagePullFormProps = $props();

	const formSchema = z.object({
		imageRef: z.string().min(1, m.images_image_required()),
		tag: z.string().optional().default('latest')
	});

	let formData = $derived({
		imageRef: '',
		tag: 'latest'
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let isPulling = $state(false);

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		isPulling = true;

		let imageName = data.imageRef.trim();
		let imageTag = data.tag?.trim() || 'latest';

		if (imageName.includes(':')) {
			const lastColonIndex = imageName.lastIndexOf(':');
			const possibleTag = imageName.substring(lastColonIndex + 1).trim();
			if (possibleTag && !possibleTag.includes('/')) {
				imageName = imageName.substring(0, lastColonIndex);
				imageTag = possibleTag;
			}
		}

		const fullImageName = `${imageName}:${imageTag}`;
		open = false;
		isPulling = false;
		void drainPullStream(fullImageName);
	}

	async function drainPullStream(fullImageName: string) {
		const result = await imageService.pullImageStream(fullImageName);
		if (!result.success) {
			const message = result.error || m.images_pull_unexpected_error();
			toast.error(message);
			onPullFinished(false, fullImageName, message);
			return;
		}
		onPullFinished(true, fullImageName);
	}

	function handleOpenChange(newOpenState: boolean) {
		if (!newOpenState && isPulling) {
			return;
		}

		open = newOpenState;
		if (!newOpenState || newOpenState) {
			$inputs.imageRef.value = '';
			$inputs.tag.value = 'latest';
		}
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.images_pull_image()}
	description={m.images_pull_description()}
	contentClass="sm:max-w-[600px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label={m.images_image_name_label()}
				type="text"
				placeholder={m.images_image_name_placeholder()}
				description={m.images_image_name_description()}
				bind:input={$inputs.imageRef}
			/>
			<FormInput
				label={m.images_tag()}
				type="text"
				placeholder={m.images_tag_latest()}
				description={m.images_tag_description()}
				bind:input={$inputs.tag}
			/>
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton action="cancel" tone="outline" type="button" class="flex-1" onclick={() => (open = false)} />
			<ArcaneButton action="pull" type="submit" class="flex-1" onclick={handleSubmit} disabled={isPulling} />
		</div>
	{/snippet}
</ResponsiveDialog.Root>
