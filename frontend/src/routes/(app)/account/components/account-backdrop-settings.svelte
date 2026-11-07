<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { Slider } from '#lib/components/ui/slider/index.js';
	import {
		BACKDROP_CUSTOM,
		BACKDROP_DEFAULT_DIM,
		BACKDROP_MAX_UPLOAD_BYTES,
		BACKDROP_NONE,
		BACKDROP_UPLOAD_TYPES,
		backdropPresets,
		backdropPresetThumbnail
	} from '#lib/config/backdrops.js';
	import { ForbiddenIcon, UploadIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { UserPreferences } from '#lib/types/user.js';
	import { cn } from '#lib/utils.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

	interface Props {
		savePreferencesDebounced: (patch: Partial<UserPreferences>) => void;
	}

	let { savePreferencesDebounced }: Props = $props();

	const currentUser = $derived(userStore.current);
	const backdropUrl = $derived(currentUser?.backdropUrl);
	const selected = $derived.by(() => {
		const value = currentUser?.preferences?.backdrop ?? BACKDROP_NONE;
		if (value === BACKDROP_CUSTOM && !backdropUrl) return BACKDROP_NONE;
		return value;
	});
	const selectedPreset = $derived(backdropPresets.find((preset) => preset.id === selected));
	let dimDraft = $state<number | null>(null);
	const dim = $derived(dimDraft ?? currentUser?.preferences?.backdropDim ?? BACKDROP_DEFAULT_DIM);
	const maxSizeMb = BACKDROP_MAX_UPLOAD_BYTES / (1024 * 1024);

	let fileInput = $state<HTMLInputElement>();
	let uploading = $state(false);
	let removing = $state(false);

	const tileClass =
		'group relative h-13 w-21 shrink-0 overflow-hidden rounded-xl border border-border/70 bg-muted/40 transition ' +
		'hover:-translate-y-0.5 hover:border-border focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ' +
		'aria-pressed:border-transparent aria-pressed:ring-2 aria-pressed:ring-primary aria-pressed:ring-offset-2 aria-pressed:ring-offset-background';
	const tileLabelClass =
		'absolute inset-x-0 bottom-0 truncate bg-linear-to-t from-black/60 to-transparent px-1.5 pt-3 pb-1 text-left text-2xs font-medium text-white';

	function select(value: string) {
		if (value === selected) return;
		void saveUserPreferences({ backdrop: value });
	}

	async function upload(event: Event & { currentTarget: HTMLInputElement }) {
		const file = event.currentTarget.files?.[0];
		event.currentTarget.value = '';
		if (!file) return;
		if (!BACKDROP_UPLOAD_TYPES.includes(file.type)) {
			toast.error(m.account_avatar_unsupported_file());
			return;
		}
		if (file.size > BACKDROP_MAX_UPLOAD_BYTES) {
			toast.error(m.background_too_large({ maxSizeMb }));
			return;
		}

		await handleApiResultWithCallbacks({
			result: await tryCatch(userService.uploadMyBackdrop(file)),
			message: m.background_upload_failed(),
			setLoadingState: (value) => (uploading = value),
			onSuccess: async (user) => {
				await userStore.setUser(user);
				await saveUserPreferences({ backdrop: BACKDROP_CUSTOM });
				toast.success(m.background_upload_success());
			}
		});
	}

	async function remove() {
		const wasSelected = selected === BACKDROP_CUSTOM;
		await handleApiResultWithCallbacks({
			result: await tryCatch(userService.deleteMyBackdrop()),
			message: m.background_remove_failed(),
			setLoadingState: (value) => (removing = value),
			onSuccess: async (user) => {
				await userStore.setUser(user);
				if (wasSelected) await saveUserPreferences({ backdrop: BACKDROP_NONE });
				toast.success(m.background_remove_success());
			}
		});
	}
</script>

<input bind:this={fileInput} type="file" accept={BACKDROP_UPLOAD_TYPES.join(',')} class="hidden" onchange={upload} />

<SettingsRow label={m.background_image()} description={m.background_image_description()} layout="wide">
	{#snippet children({ describedBy })}
		<div class="flex flex-wrap gap-3" role="group" aria-label={m.background_image()} aria-describedby={describedBy}>
			<button
				type="button"
				class={cn(
					tileClass,
					'flex flex-col items-center justify-center gap-0.5 border-dashed bg-transparent text-muted-foreground hover:text-foreground'
				)}
				disabled={uploading}
				aria-busy={uploading}
				onclick={() => fileInput?.click()}
			>
				<UploadIcon class={cn('size-4', uploading && 'animate-pulse')} />
				<span class="text-2xs font-medium">{m.upload()}</span>
			</button>
			{#if backdropUrl}
				<button
					type="button"
					class={tileClass}
					aria-pressed={selected === BACKDROP_CUSTOM}
					onclick={() => select(BACKDROP_CUSTOM)}
				>
					<img src={backdropUrl} alt="" class="size-full object-cover" />
					<span class={tileLabelClass}>{m.uploaded()}</span>
				</button>
			{/if}
			{#each backdropPresets as preset (preset.id)}
				<button type="button" class={tileClass} aria-pressed={selected === preset.id} onclick={() => select(preset.id)}>
					<img src={backdropPresetThumbnail(preset.id)} alt="" loading="lazy" class="size-full object-cover" />
					<span class={tileLabelClass}>{preset.label()}</span>
				</button>
			{/each}
			<button
				type="button"
				class={cn(tileClass, 'flex flex-col items-center justify-center gap-0.5 text-muted-foreground')}
				aria-pressed={selected === BACKDROP_NONE}
				onclick={() => select(BACKDROP_NONE)}
			>
				<ForbiddenIcon class="size-4" />
				<span class="text-2xs font-medium">{m.none()}</span>
			</button>
		</div>
	{/snippet}
	{#snippet helpText()}
		{#if selectedPreset}
			{m.photo_by()}
			<a href={selectedPreset.photographerUrl} target="_blank" rel="noopener noreferrer" class="underline underline-offset-2">
				{selectedPreset.photographer}
			</a>
			{m.on_unsplash()}
		{/if}
	{/snippet}
</SettingsRow>

{#if backdropUrl}
	<SettingsRow label={m.uploaded_background()} description={m.uploaded_background_description({ maxSizeMb })}>
		{#snippet children()}
			<div class="flex flex-wrap items-center gap-3">
				<img src={backdropUrl} alt={m.uploaded_background()} class="h-13 w-21 rounded-xl border border-border/70 object-cover" />
				<ArcaneButton
					action="base"
					size="sm"
					icon={UploadIcon}
					customLabel={m.replace()}
					loading={uploading}
					disabled={uploading || removing}
					onclick={() => fileInput?.click()}
				/>
				<ArcaneButton action="remove" size="sm" loading={removing} disabled={uploading || removing} onclick={remove} />
			</div>
		{/snippet}
	</SettingsRow>
{/if}

<SettingsRow label={m.background_dim()} description={m.background_dim_description()}>
	{#snippet children({ describedBy })}
		<div class="flex items-center gap-4">
			<Slider
				type="single"
				value={dim}
				min={0}
				max={80}
				step={5}
				disabled={selected === BACKDROP_NONE}
				onValueChange={(value) => (dimDraft = value)}
				onValueCommit={(value) => {
					savePreferencesDebounced({ backdropDim: value });
					dimDraft = null;
				}}
				aria-label={m.background_dim()}
				aria-describedby={describedBy}
				class="max-w-64"
			/>
			<span class="w-10 text-right text-xs text-muted-foreground tabular-nums">{dim}%</span>
		</div>
	{/snippet}
</SettingsRow>
