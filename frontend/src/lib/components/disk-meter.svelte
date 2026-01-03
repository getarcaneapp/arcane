<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Popover from '$lib/components/ui/popover';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { m } from '$lib/paraglide/messages';
	import bytes from 'bytes';
	import settingsStore from '$lib/stores/config-store';
	import { settingsService } from '$lib/services/settings-service';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';
	import { VolumesIcon, SettingsIcon } from '$lib/icons';

	let {
		diskUsage,
		diskTotal,
		loading = false,
		class: className
	}: {
		diskUsage?: number;
		diskTotal?: number;
		loading?: boolean;
		class?: string;
	} = $props();

	const percentage = $derived(
		!loading && diskUsage !== undefined && diskTotal !== undefined && diskTotal > 0 ? (diskUsage / diskTotal) * 100 : 0
	);

	const diskFree = $derived(diskUsage !== undefined && diskTotal !== undefined ? diskTotal - diskUsage : 0);

	let diskUsagePath = $state($settingsStore.diskUsagePath || 'data/projects');
	let popoverOpen = $state(false);
	let isSaving = $state(false);

	const pathSchema = z
		.string()
		.min(1, 'Path cannot be empty')
		.refine((path) => !path.includes('..'), 'Path cannot contain ".."')
		.refine((path) => !/^[a-zA-Z]:/.test(path), 'Windows-style paths are not supported');

	async function saveDiskUsagePath() {
		const trimmedPath = diskUsagePath.trim();
		const result = pathSchema.safeParse(trimmedPath);

		if (!result.success) {
			const firstError = result.error.issues[0];
			toast.error(firstError.message);
			return;
		}

		isSaving = true;

		try {
			await settingsService.updateSettings({ diskUsagePath: trimmedPath });
			settingsStore.set({ ...$settingsStore, diskUsagePath: trimmedPath });
			toast.success(m.disk_usage_save());
			popoverOpen = false;
		} catch (error) {
			console.error('Failed to update disk usage path:', error);
			toast.error(m.disk_usage_save_failed());
		} finally {
			isSaving = false;
		}
	}

	function formatBytes(value: number): string {
		return bytes.format(value, { unitSeparator: ' ' }) ?? '-';
	}
</script>

<Card.Root class="flex h-full flex-col {className}">
	{#snippet children()}
		<Card.Header icon={VolumesIcon} iconVariant="primary" compact {loading}>
			{#snippet children()}
				<div class="min-w-0 flex-1">
					<div class="text-foreground text-sm font-semibold">{m.dashboard_meter_disk()}</div>
					<div class="text-muted-foreground text-xs">{m.dashboard_meter_disk_desc()}</div>
					<div class="text-muted-foreground/70 mt-0.5 font-mono text-[10px]">
						{m.dashboard_meter_disk_monitoring({ path: $settingsStore.diskUsagePath })}
					</div>
				</div>
				<Popover.Root bind:open={popoverOpen}>
					<Popover.Trigger>
						{#snippet child({ props })}
							<ArcaneButton
								{...props}
								action="base"
								tone="ghost"
								size="icon"
								icon={SettingsIcon}
								class="hover:bg-muted size-7 shrink-0"
							/>
						{/snippet}
					</Popover.Trigger>
					<Popover.Content class="w-80">
						<div class="space-y-4">
							<div class="space-y-2">
								<h4 class="text-sm leading-none font-medium">{m.disk_usage_settings()}</h4>
								<p class="text-muted-foreground text-sm">{m.disk_usage_settings_description()}</p>
							</div>
							<div class="space-y-2">
								<Label for="disk-path">{m.directory_path()}</Label>
								<Input id="disk-path" placeholder="data/projects" bind:value={diskUsagePath} disabled={isSaving} />
							</div>
							<div class="flex justify-end gap-2">
								<ArcaneButton
									action="cancel"
									size="sm"
									onclick={() => {
										diskUsagePath = $settingsStore.diskUsagePath || 'data/projects';
										popoverOpen = false;
									}}
									disabled={isSaving}
								/>
								<ArcaneButton action="save" size="sm" onclick={saveDiskUsagePath} disabled={isSaving} loading={isSaving} />
							</div>
						</div>
					</Popover.Content>
				</Popover.Root>
			{/snippet}
		</Card.Header>

		<Card.Content class="flex flex-1 flex-col justify-end gap-3 p-3 sm:p-4">
			{#if loading}
				<div class="flex items-center gap-3">
					<div class="bg-muted h-2.5 flex-1 animate-pulse rounded-full"></div>
					<div class="bg-muted h-4 w-12 animate-pulse rounded"></div>
				</div>
				<div class="flex justify-between gap-4">
					<div class="bg-muted h-4 w-20 animate-pulse rounded"></div>
					<div class="bg-muted h-4 w-20 animate-pulse rounded"></div>
				</div>
			{:else}
				<div class="flex items-center gap-3">
					<Progress value={percentage} max={100} class="h-2.5 flex-1" />
					<span class="text-foreground min-w-12 text-right text-sm font-bold tabular-nums">
						{percentage.toFixed(1)}%
					</span>
				</div>

				<div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
					<div class="flex items-center gap-2 whitespace-nowrap">
						<div class="bg-primary size-2 shrink-0 rounded-full"></div>
						<span class="text-muted-foreground text-xs">{m.dashboard_meter_disk_used()}</span>
						<span class="text-foreground text-sm font-semibold tabular-nums">{formatBytes(diskUsage ?? 0)}</span>
					</div>
					<div class="flex items-center gap-2 whitespace-nowrap">
						<div class="bg-primary/20 size-2 shrink-0 rounded-full"></div>
						<span class="text-muted-foreground text-xs">{m.dashboard_meter_disk_free()}</span>
						<span class="text-foreground text-sm font-semibold tabular-nums">{formatBytes(diskFree)}</span>
					</div>
				</div>
			{/if}
		</Card.Content>
	{/snippet}
</Card.Root>
