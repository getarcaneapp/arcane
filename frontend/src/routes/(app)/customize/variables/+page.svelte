<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { toast } from 'svelte-sonner';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import XIcon from '@lucide/svelte/icons/x';
	import SearchIcon from '@lucide/svelte/icons/search';
	import KeyIcon from '@lucide/svelte/icons/key';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { templateService } from '$lib/services/template-service.js';
	import type { Variable } from '$lib/types/variable.type';
	import { untrack } from 'svelte';
	import { m } from '$lib/paraglide/messages';

	let { data } = $props();
	let envVars = $state<Variable[]>(untrack(() => [...data.variables]));
	let originalVars = $state<Variable[]>(untrack(() => [...data.variables]));
	let searchQuery = $state('');
	let isLoading = $state(false);

	const filteredVars = $derived.by(() => {
		if (!searchQuery.trim()) return envVars;

		const query = searchQuery.toLowerCase();
		return envVars.filter((v) => v.key.toLowerCase().includes(query) || v.value.toLowerCase().includes(query));
	});

	const hasChanges = $derived.by(() => {
		if (envVars.length !== originalVars.length) return true;

		const originalMap = new Map(originalVars.map((v) => [v.key, v.value]));

		for (const v of envVars) {
			if (!v.key.trim()) continue;

			const originalValue = originalMap.get(v.key);
			if (originalValue !== v.value) return true;
			if (originalValue === undefined) return true;
		}

		const currentMap = new Map(envVars.filter((v) => v.key.trim()).map((v) => [v.key, v.value]));
		for (const key of originalMap.keys()) {
			if (!currentMap.has(key)) return true;
		}

		return false;
	});

	function addEnvVar() {
		envVars = [...envVars, { key: '', value: '' }];
	}

	function removeEnvVar(index: number) {
		envVars = envVars.filter((_, i) => i !== index);
	}

	function duplicateEnvVar(index: number) {
		const varToDuplicate = envVars[index];
		let baseKey = varToDuplicate.key;
		let counter = 1;

		// Check if key already ends with _# pattern
		const match = baseKey.match(/^(.+)_(\d+)$/);
		if (match) {
			baseKey = match[1];
			counter = parseInt(match[2], 10) + 1;
		}

		let newKey = `${baseKey}_${counter}`;

		// Find next available number
		while (envVars.some((v) => v.key === newKey)) {
			counter++;
			newKey = `${baseKey}_${counter}`;
		}

		const newVar = {
			key: newKey,
			value: varToDuplicate.value
		};
		envVars = [...envVars.slice(0, index + 1), newVar, ...envVars.slice(index + 1)];
	}

	async function onSubmit() {
		isLoading = true;

		try {
			const validVars = envVars
				.filter((v) => v.key.trim() !== '')
				.map((v) => ({
					key: v.key.trim(),
					value: v.value.trim()
				}));

			const keys = validVars.map((v) => v.key);
			const uniqueKeys = new Set(keys);
			if (keys.length !== uniqueKeys.size) {
				toast.error(m.variables_duplicate_keys_error());
				return;
			}

			await templateService.updateGlobalVariables(validVars);

			originalVars = [...validVars];
			envVars = [...validVars];

			toast.success(m.variables_save_success());
		} catch (error) {
			console.error('Failed to save global environment variables:', error);
			toast.error(m.variables_save_failed());
		} finally {
			isLoading = false;
		}
	}

	function resetForm() {
		envVars = [...originalVars];
		searchQuery = '';
	}

	function handleKeyDown(event: KeyboardEvent, index: number) {
		if (event.key === 'Enter') {
			event.preventDefault();
			addEnvVar();
		}
	}

	$effect(() => {
		if (envVars.length === 0) {
			addEnvVar();
		}
	});

	const actionButtons = $derived<ActionButton[]>([
		{
			id: 'reset',
			action: 'restart',
			label: m.common_reset(),
			onclick: resetForm,
			disabled: !hasChanges || isLoading
		},
		{
			id: 'save',
			action: 'save',
			label: m.common_save(),
			loadingLabel: m.common_saving(),
			loading: isLoading,
			disabled: isLoading || !hasChanges,
			onclick: onSubmit
		}
	]);
</script>

<ResourcePageLayout title={m.variables_title()} subtitle={m.variables_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<fieldset class="relative">
			<div class="space-y-4 sm:space-y-6">
				<Card.Root class="border-primary/20 bg-primary/5 overflow-hidden pt-0">
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="flex items-start gap-3">
							<div class="text-primary mt-0.5 shrink-0">
								<AlertCircleIcon class="size-5" />
							</div>
							<div class="space-y-1 text-sm">
								<p class="text-foreground font-medium">{m.variables_about_title()}</p>
								<p class="text-muted-foreground">
									{m.variables_about_description()}
								</p>
							</div>
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root class="overflow-hidden pt-0">
					<Card.Header class="bg-muted/20 sticky top-0 z-10 border-b !py-3">
						<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
							<div class="flex items-center gap-3">
								<div class="bg-primary/10 text-primary ring-primary/20 flex size-8 items-center justify-center rounded-lg ring-1">
									<KeyIcon class="size-4" />
								</div>
								<div>
									<Card.Title class="text-base">{m.common_environment_variables()}</Card.Title>
									<Card.Description class="text-xs">
										{#if envVars.filter((v) => v.key.trim()).length === 1}
											{m.variables_count_configured({ count: 1 })}
										{:else}
											{m.variables_count_configured_plural({ count: envVars.filter((v) => v.key.trim()).length })}
										{/if}
									</Card.Description>
								</div>
							</div>

							<div class="flex items-center gap-2">
								<div class="relative w-full sm:w-52">
									<SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
									<Input type="text" placeholder={m.common_search()} bind:value={searchQuery} class="h-9 pl-10" />
								</div>
								<Button type="button" size="sm" onclick={addEnvVar} disabled={isLoading} class="shrink-0">
									<PlusIcon class="mr-1.5 size-4" />
									{m.variables_add_button()}
								</Button>
							</div>
						</div>
					</Card.Header>

					<Card.Content class="px-3 py-4 sm:px-6">
						{#if filteredVars.length === 0 && searchQuery.trim()}
							<div class="text-muted-foreground flex flex-col items-center justify-center py-12 text-center">
								<SearchIcon class="mb-3 size-12 opacity-20" />
								<p class="text-sm font-medium">{m.variables_no_results_title()}</p>
								<p class="text-xs">{m.variables_no_results_description()}</p>
							</div>
						{:else if filteredVars.length === 0}
							<div class="text-muted-foreground flex flex-col items-center justify-center py-12 text-center">
								<KeyIcon class="mb-3 size-12 opacity-20" />
								<p class="text-sm font-medium">{m.variables_no_variables_title()}</p>
								<p class="text-xs">{m.variables_no_variables_description()}</p>
							</div>
						{:else}
							<div class="space-y-2">
								{#each filteredVars as envVar, index (index)}
									{@const actualIndex = envVars.indexOf(envVar)}
									<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
										<div class="flex flex-1 items-center gap-2">
											<InputGroup.Root class="flex-1 sm:max-w-[200px]">
												<InputGroup.Input
													type="text"
													placeholder={m.variables_key_placeholder()}
													bind:value={envVar.key}
													disabled={isLoading}
													class="font-mono text-sm"
													onkeydown={(e) => handleKeyDown(e, actualIndex)}
													oninput={(e) => {
														const target = e.target as HTMLInputElement;
														const cursorPos = target.selectionStart || 0;
														const oldValue = envVar.key;
														const newValue = target.value.toUpperCase().replace(/\s/g, '_');

														envVar.key = newValue;

														requestAnimationFrame(() => {
															const diff = newValue.length - oldValue.length;
															target.setSelectionRange(cursorPos + diff, cursorPos + diff);
														});
													}}
												/>
											</InputGroup.Root>
											<span class="text-muted-foreground font-mono text-sm">=</span>
											<InputGroup.Root class="flex-[2]">
												<InputGroup.Input
													type="text"
													placeholder={m.variables_value_placeholder()}
													bind:value={envVar.value}
													disabled={isLoading}
													class="font-mono text-sm"
													onkeydown={(e) => handleKeyDown(e, actualIndex)}
												/>
												<InputGroup.Addon align="inline-end">
													<Tooltip.Provider>
														<Tooltip.Root>
															<Tooltip.Trigger>
																{#snippet child({ props })}
																	<InputGroup.Button
																		{...props}
																		variant="ghost"
																		aria-label={m.duplicate()}
																		size="icon-xs"
																		onclick={() => duplicateEnvVar(actualIndex)}
																		disabled={isLoading}
																	>
																		<CopyIcon />
																	</InputGroup.Button>
																{/snippet}
															</Tooltip.Trigger>
															<Tooltip.Content>
																<p>{m.duplicate()}</p>
															</Tooltip.Content>
														</Tooltip.Root>
													</Tooltip.Provider>
													<Tooltip.Provider>
														<Tooltip.Root>
															<Tooltip.Trigger>
																{#snippet child({ props })}
																	<InputGroup.Button
																		{...props}
																		variant="ghost"
																		aria-label={m.common_remove()}
																		size="icon-xs"
																		onclick={() => removeEnvVar(actualIndex)}
																		disabled={isLoading}
																		class="text-destructive hover:text-destructive"
																	>
																		<XIcon />
																	</InputGroup.Button>
																{/snippet}
															</Tooltip.Trigger>
															<Tooltip.Content>
																<p>{m.common_remove()}</p>
															</Tooltip.Content>
														</Tooltip.Root>
													</Tooltip.Provider>
												</InputGroup.Addon>
											</InputGroup.Root>
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</Card.Content>
				</Card.Root>
			</div>
		</fieldset>
	{/snippet}
</ResourcePageLayout>
