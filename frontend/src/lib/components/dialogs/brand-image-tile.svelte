<script module lang="ts">
	export const BRAND_IMAGE_MAX_SIZE_MB = 2;
</script>

<script lang="ts">
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { UploadIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import type { BrandImageSlot } from '#lib/types/settings.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	interface Props {
		slot: BrandImageSlot;
		label: string;
		hint: string;
		previewUrl: string;
		custom: boolean;
		accept: string[];
		square?: { min: number; error: string };
	}

	let { slot, label, hint, previewUrl, custom, accept, square }: Props = $props();

	const queryClient = useQueryClient();

	let fileInput = $state<HTMLInputElement>();
	let pending = $state<'upload' | 'remove' | null>(null);

	async function refresh() {
		await settingsStore.reload();
		await queryClient.invalidateQueries({ queryKey: queryKeys.settings.public(LOCAL_DOCKER_ENVIRONMENT_ID) });
	}

	async function upload(event: Event & { currentTarget: HTMLInputElement }) {
		const file = event.currentTarget.files?.[0];
		event.currentTarget.value = '';
		if (!file) return;
		const name = file.name.toLowerCase();
		if (!accept.some((type) => (type.startsWith('.') ? name.endsWith(type) : file.type === type))) {
			toast.error(m.unsupported_file_type(), { description: hint });
			return;
		}
		if (file.size > BRAND_IMAGE_MAX_SIZE_MB * 1024 * 1024) {
			toast.error(m.image_too_large({ maxSizeMb: BRAND_IMAGE_MAX_SIZE_MB }));
			return;
		}
		if (square) {
			const bitmap = await tryCatch(createImageBitmap(file));
			const valid = !!bitmap.data && bitmap.data.width === bitmap.data.height && bitmap.data.width >= square.min;
			bitmap.data?.close();
			if (!valid) {
				toast.error(square.error);
				return;
			}
		}

		if (pending !== null) return;
		pending = 'upload';
		try {
			await handleApiResultWithCallbacks({
				result: await tryCatch(settingsService.uploadBrandImage(slot, file)),
				message: m.images_upload_failed(),
				onSuccess: async () => {
					await refresh();
					toast.success(m.images_upload_success());
				}
			});
		} finally {
			pending = null;
		}
	}

	async function remove() {
		if (pending !== null) return;
		pending = 'remove';
		try {
			await handleApiResultWithCallbacks({
				result: await tryCatch(settingsService.deleteBrandImage(slot)),
				message: m.common_remove_failed({ resource: label }),
				onSuccess: async () => {
					await refresh();
					toast.success(m.common_remove_success({ resource: label }));
				}
			});
		} finally {
			pending = null;
		}
	}
</script>

<input bind:this={fileInput} type="file" accept={accept.join(',')} class="hidden" onchange={upload} />

<div class="flex min-w-0 flex-col gap-3 rounded-xl border border-border/70 bg-card/10 p-3">
	<div class="flex h-20 items-center justify-center rounded-xl border border-border/50 bg-card/10 p-3">
		<img src={previewUrl} alt={label} class="max-h-full max-w-full object-contain" />
	</div>
	<div class="flex flex-col gap-0.5">
		<span class="text-sm font-medium text-foreground">{label}</span>
		<span class="text-xs text-muted-foreground">{hint}</span>
	</div>
	<div class="flex flex-wrap items-center gap-2">
		<ArcaneButton
			action="base"
			tone="outline"
			icon={UploadIcon}
			customLabel={custom ? m.replace() : m.upload()}
			loading={pending === 'upload'}
			disabled={pending !== null || settingsStore.readOnly}
			onclick={() => fileInput?.click()}
		/>
		{#if custom}
			<ArcaneButton
				action="remove"
				loading={pending === 'remove'}
				disabled={pending !== null || settingsStore.readOnly}
				onclick={remove}
			/>
		{/if}
	</div>
</div>
