<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { toast } from 'svelte-sonner';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import { readNdjsonStream } from '$lib/utils/streaming';

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
		const envId = await environmentStore.getCurrentEnvironmentId();

		try {
			const response = await fetch(`/api/environments/${envId}/images/pull`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ imageName: fullImageName })
			});

			if (!response.ok || !response.body) {
				const errorData = await response.json().catch(() => ({
					data: { message: m.images_pull_server_error() }
				}));
				const errorMessage =
					errorData.data?.message ||
					errorData.error ||
					errorData.message ||
					`${m.images_pull_server_error()}: HTTP ${response.status}`;
				throw new Error(errorMessage);
			}

			open = false;
			isPulling = false;

			drainPullStream(response.body, fullImageName);
		} catch (error: any) {
			const message = error.message || m.images_pull_unexpected_error();
			toast.error(message);
			onPullFinished(false, fullImageName, message);
			isPulling = false;
		}
	}

	function drainPullStream(body: ReadableStream<Uint8Array>, fullImageName: string) {
		(async () => {
			let streamFailed = false;
			try {
				await readNdjsonStream(body, (parsed) => {
					if (parsed?.error) {
						const errMsg = typeof parsed.error === 'string' ? parsed.error : parsed.error.message || m.images_pull_stream_error();
						streamFailed = true;
						toast.error(errMsg);
						onPullFinished(false, fullImageName, errMsg);
						throw new Error(errMsg);
					}
				});
				onPullFinished(true, fullImageName);
			} catch (error: any) {
				if (streamFailed) return;
				const message = error.message || m.images_pull_unexpected_error();
				toast.error(message);
				onPullFinished(false, fullImageName, message);
			}
		})();
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
