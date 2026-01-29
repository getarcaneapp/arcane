<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { toast } from 'svelte-sonner';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils.js';
	import {
		type LayerProgress,
		type PullPhase,
		calculateOverallProgress,
		areAllLayersComplete,
		updateLayerFromStreamData,
		extractErrorMessage,
		getLayerStats,
		getPullPhase,
		showImageLayersState,
		isIndeterminatePhase,
		getAggregateStatus
	} from '$lib/utils/pull-progress';
	import { ArrowDownIcon, SuccessIcon, DownloadIcon } from '$lib/icons';

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
	let pullProgress = $state(0);
	let pullStatusText = $state('');
	let pullError = $state('');
	let layerProgress = $state<Record<string, LayerProgress>>({});
	let hasReachedComplete = $state(false);
	let currentImageName = $state('');
	const layerStats = $derived(getLayerStats(layerProgress, hasReachedComplete));
	const aggregateStatus = $derived(getAggregateStatus(layerProgress, pullStatusText, hasReachedComplete));
	const showPullUI = $derived(isPulling || hasReachedComplete || !!pullError);
	const isIndeterminate = $derived(isIndeterminatePhase(layerProgress, pullProgress));
	let prevOpen = $state(false);

	$effect(() => {
		if (prevOpen && !open && !isPulling) {
			// Sheet just closed, reset state and form
			resetState();
			$inputs.imageRef.value = '';
			$inputs.tag.value = 'latest';
		}
		prevOpen = open;
	});

	function getLayerPhase(status: string): PullPhase {
		return getPullPhase(status, false, false);
	}

	function resetState() {
		isPulling = false;
		pullProgress = 0;
		pullStatusText = '';
		pullError = '';
		layerProgress = {};
		hasReachedComplete = false;
		currentImageName = '';
	}

	function updateProgress() {
		pullProgress = calculateOverallProgress(layerProgress);
	}

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		resetState();
		isPulling = true;
		pullStatusText = m.images_pull_initiating();

		let imageName = data.imageRef.trim();
		let imageTag = data.tag?.trim() || 'latest';

		if (imageName.includes(':')) {
			const lastColonIndex = imageName.lastIndexOf(':');
			const possibleTag = imageName.substring(lastColonIndex + 1).trim();
			// Only split if the part after the last colon looks like a tag (not a port number in a registry URL)
			if (possibleTag && !possibleTag.includes('/')) {
				imageName = imageName.substring(0, lastColonIndex);
				imageTag = possibleTag;
			}
		}

		const fullImageName = `${imageName}:${imageTag}`;
		currentImageName = fullImageName;
		const envId = await environmentStore.getCurrentEnvironmentId();
		pullStatusText = m.images_pull_initiating();

		try {
			const response = await fetch(`/api/environments/${envId}/images/pull`, {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
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

			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			let buffer = '';

			while (true) {
				const { done, value } = await reader.read();
				if (done) {
					pullStatusText = m.images_pull_processing_final_layers();
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() || '';

				for (const line of lines) {
					if (line.trim() === '') continue;
					try {
						const data = JSON.parse(line);

						const errorMsg = extractErrorMessage(data, m.images_pull_stream_error());
						if (errorMsg) {
							console.error('Error in stream:', errorMsg);
							pullError = errorMsg;
							pullStatusText = m.images_pull_failed_with_error({ error: pullError });
							continue;
						}

						if (data.status) pullStatusText = data.status;
						layerProgress = updateLayerFromStreamData(layerProgress, data);
						updateProgress();
					} catch (e: any) {
						console.warn('Failed to parse stream line or process data:', line, e);
					}
				}
			}

			updateProgress();
			if (!pullError && pullProgress < 100 && areAllLayersComplete(layerProgress)) {
				pullProgress = 100;
			}

			if (pullError) {
				throw new Error(pullError);
			}

			hasReachedComplete = true;
			pullProgress = 100;
			pullStatusText = m.images_pull_success({ repoTag: fullImageName });
			toast.success(m.images_pull_success({ repoTag: fullImageName }));
			onPullFinished(true, fullImageName);

			// Close sheet after a brief delay to show success state
			// State will be reset by handleOpenChange when sheet closes
			setTimeout(() => {
				open = false;
			}, 1500);
		} catch (error: any) {
			console.error('Pull image error:', error);
			const message = error.message || m.images_pull_unexpected_error();
			pullError = message;
			pullStatusText = m.images_pull_failed_with_error({ error: message });
			toast.error(message);
			onPullFinished(false, fullImageName, message);
		} finally {
			isPulling = false;
		}
	}

	function handleOpenChange(newOpenState: boolean) {
		if (!newOpenState && isPulling) {
			toast.info(m.images_pull_in_progress_toast());
			open = true; // Keep it open
			return;
		}

		open = newOpenState;
		if (!newOpenState) {
			// Reset when closing (if we get here, isPulling is false)
			resetState();
			$inputs.imageRef.value = '';
			$inputs.tag.value = 'latest';
		} else {
			// Also reset when opening to ensure clean state
			resetState();
			$inputs.imageRef.value = '';
			$inputs.tag.value = 'latest';
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.images_pull_image()}
	description={showPullUI && currentImageName ? currentImageName : m.images_pull_description()}
	contentClass="sm:max-w-[600px]"
>
	{#snippet children()}
		{#if showPullUI}
			<!-- Pull progress UI -->
			<div class="space-y-4 py-6">
				{#if pullError}
					<div class="bg-destructive/10 text-destructive rounded-lg p-4">
						<p class="text-sm font-medium">{m.image_update_error_label()}</p>
						<p class="mt-1 text-xs">{pullError}</p>
					</div>
				{:else}
					<!-- Progress status -->
					<div class="space-y-2">
						<div class="flex items-center justify-between">
							<p class="text-sm font-medium">
								{#if hasReachedComplete}
									{m.progress_pull_completed()}
								{:else if layerStats.total > 0}
									<span class="flex items-center gap-1.5">
										<span>{aggregateStatus}</span>
										<span class="text-muted-foreground font-normal">•</span>
										<span class="text-muted-foreground font-normal">
											{m.progress_layers_status({ completed: layerStats.completed, total: layerStats.total })}
										</span>
									</span>
								{:else}
									{aggregateStatus || m.common_action_pulling()}
								{/if}
							</p>
							{#if !isIndeterminate || hasReachedComplete}
								<p class="text-muted-foreground text-sm">{Math.round(hasReachedComplete ? 100 : pullProgress)}%</p>
							{/if}
						</div>
						<Progress
							value={hasReachedComplete || isIndeterminate ? 100 : pullProgress}
							max={100}
							class="h-2 w-full"
							indeterminate={isIndeterminate && !hasReachedComplete}
						/>
					</div>

					{#if Object.keys(layerProgress).length > 0}
						<Collapsible.Root bind:open={showImageLayersState.current}>
							<Collapsible.Trigger
								class="text-muted-foreground hover:text-foreground hover:bg-accent flex w-full items-center justify-between rounded-md px-2 py-1.5 text-xs transition-colors"
							>
								{m.progress_show_layers()}
								<ArrowDownIcon class={cn('size-4 transition-transform', showImageLayersState.current && 'rotate-180')} />
							</Collapsible.Trigger>
							<Collapsible.Content>
								<div class="mt-2 space-y-1.5">
									{#each Object.entries(layerProgress) as [id, layer] (id)}
										{@const phase = hasReachedComplete ? 'complete' : getLayerPhase(layer.status)}
										{@const layerPercent =
											phase === 'complete' ? 100 : layer.total > 0 ? Math.round((layer.current / layer.total) * 100) : 0}
										<div class="bg-muted/30 rounded-md px-2 py-1.5">
											<div class="flex items-center justify-between gap-2">
												<span class="text-muted-foreground truncate font-mono text-[10px]">{id.slice(0, 12)}</span>
												<span
													class={cn(
														'shrink-0 text-[10px] font-medium',
														phase === 'complete' && 'text-green-500',
														phase === 'downloading' && 'text-blue-500',
														phase === 'extracting' && 'text-amber-500'
													)}
												>
													{#if phase === 'complete'}
														✓
													{:else if layer.total > 0}
														{layerPercent}%
													{:else}
														{layer.status}
													{/if}
												</span>
											</div>
											<Progress value={layerPercent} max={100} class="mt-1 h-1" />
										</div>
									{/each}
								</div>
							</Collapsible.Content>
						</Collapsible.Root>
					{/if}

					{#if isPulling}
						<p class="text-muted-foreground text-xs">{m.images_pull_wait_message()}</p>
					{/if}
				{/if}
			</div>
		{:else}
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
		{/if}
	{/snippet}

	{#snippet footer()}
		{#if pullError}
			<div class="flex w-full flex-row gap-2">
				<ArcaneButton
					action="base"
					tone="outline"
					type="button"
					class="flex-1"
					onclick={() => resetState()}
					customLabel={m.common_retry()}
				/>
				<ArcaneButton action="base" type="button" class="flex-1" onclick={() => (open = false)} customLabel={m.common_close()} />
			</div>
		{:else if !showPullUI}
			<div class="flex w-full flex-row gap-2">
				<ArcaneButton action="cancel" tone="outline" type="button" class="flex-1" onclick={() => (open = false)} />
				<ArcaneButton action="pull" type="submit" class="flex-1" onclick={handleSubmit} />
			</div>
		{/if}
	{/snippet}
</ResponsiveDialog.Root>
