<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { CloseIcon, EnvironmentsIcon, SmartphoneIcon, MonitorSpeakerIcon, DockIcon, type IconType } from '$lib/icons';

	let {
		id,
		description,
		serverValue,
		localOverride,
		onServerChange,
		onLocalOverride,
		onClearOverride,
		serverDisabled = false
	}: {
		id: string;
		description: string;
		serverValue: 'floating' | 'docked';
		localOverride?: 'floating' | 'docked';
		onServerChange: (value: 'floating' | 'docked') => void;
		onLocalOverride: (value: 'floating' | 'docked') => void;
		onClearOverride: () => void;
		serverDisabled?: boolean;
	} = $props();

	const effectiveValue = $derived(localOverride !== undefined ? localOverride : serverValue);
	const hasOverride = $derived(localOverride !== undefined);
</script>

<div
	class={`flex h-full flex-col rounded-lg border p-3 sm:p-4 ${hasOverride ? 'border-orange-200 bg-orange-50/50 dark:border-orange-800 dark:bg-orange-950/20' : 'border-border'}`}
>
	<div class="flex h-full flex-col space-y-3">
		<div class="flex items-start gap-3">
			<div class="min-w-0 flex-1">
				<div class="mb-1 flex items-start justify-between gap-2">
					{#if hasOverride}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							onclick={onClearOverride}
							class="h-6 w-6 flex-shrink-0 p-0 text-orange-600 hover:text-orange-700 dark:text-orange-400 dark:hover:text-orange-300"
							title={m.clear_local_override()}
						>
							<CloseIcon class="size-3" />
						</ArcaneButton>
					{/if}
				</div>
				<p class="text-muted-foreground text-xs leading-relaxed">{description}</p>
			</div>
		</div>

		<div class="flex flex-1 flex-col justify-end space-y-2 sm:space-y-3">
			<div class="bg-background/50 rounded-md border">
				<div class="flex items-center justify-between p-2 sm:p-3">
					<div class="flex min-w-0 flex-1 items-center gap-2">
						<EnvironmentsIcon class="text-muted-foreground size-3 flex-shrink-0 sm:size-4" />
						<div class="min-w-0 flex-1">
							<p class="text-xs leading-tight font-medium">{m.server_default()}</p>
							<p class="text-muted-foreground hidden text-xs leading-tight sm:block">{m.applies_to_all_users()}</p>
						</div>
					</div>

					<div class="flex flex-shrink-0 gap-1">
						<ArcaneButton
							action="base"
							tone={serverValue === 'floating' ? 'outline-primary' : 'outline'}
							size="sm"
							onclick={() => !serverDisabled && onServerChange('floating')}
							disabled={serverDisabled}
							class="flex h-7 min-w-[2.5rem] items-center gap-1 px-2 text-xs sm:h-8 sm:px-3"
						>
							<MonitorSpeakerIcon class="size-3" />
							<span class="hidden sm:inline">{m.navigation_mode_floating()}</span>
						</ArcaneButton>
						<ArcaneButton
							action="base"
							tone={serverValue === 'docked' ? 'outline-primary' : 'outline'}
							size="sm"
							onclick={() => !serverDisabled && onServerChange('docked')}
							disabled={serverDisabled}
							class="flex h-7 min-w-[2.5rem] items-center gap-1 px-2 text-xs sm:h-8 sm:px-3"
						>
							<DockIcon class="size-3" />
							<span class="hidden sm:inline">{m.navigation_mode_docked()}</span>
						</ArcaneButton>
					</div>
				</div>
			</div>

			<div
				class={`rounded-md border ${hasOverride ? 'border-orange-200 bg-orange-50 dark:border-orange-800 dark:bg-orange-950/30' : 'bg-muted/30 border-border'}`}
			>
				<div class="flex items-center justify-between p-2 sm:p-3">
					<div class="flex min-w-0 flex-1 items-center gap-2">
						<SmartphoneIcon class="text-muted-foreground size-3 flex-shrink-0 sm:size-4" />
						<div class="min-w-0 flex-1">
							<div class="flex flex-col sm:flex-row sm:items-center sm:gap-1">
								<p class="text-xs leading-tight font-medium">{m.this_device()}</p>
								{#if hasOverride}
									<span class="text-xs leading-tight text-orange-600 dark:text-orange-400">({m.override()})</span>
								{/if}
							</div>
							<p class="text-muted-foreground hidden text-xs leading-tight sm:block">
								{hasOverride ? m.overriding_server_default() : m.using_server_default()}
							</p>
						</div>
					</div>

					{#if hasOverride}
						<div class="flex flex-shrink-0 gap-1">
							<ArcaneButton
								action="base"
								tone={localOverride === 'floating' ? 'outline-primary' : 'outline'}
								size="sm"
								onclick={() => onLocalOverride('floating')}
								class="flex h-7 min-w-[2.5rem] items-center gap-1 px-2 text-xs sm:h-8 sm:px-3"
							>
								<MonitorSpeakerIcon class="size-3" />
								<span class="hidden sm:inline">{m.navigation_mode_floating()}</span>
							</ArcaneButton>
							<ArcaneButton
								action="base"
								tone={localOverride === 'docked' ? 'outline-primary' : 'outline'}
								size="sm"
								onclick={() => onLocalOverride('docked')}
								class="flex h-7 min-w-[2.5rem] items-center gap-1 px-2 text-xs sm:h-8 sm:px-3"
							>
								<DockIcon class="size-3" />
								<span class="hidden sm:inline">{m.navigation_mode_docked()}</span>
							</ArcaneButton>
						</div>
					{:else}
						<div class="flex flex-shrink-0 items-center gap-2">
							<span class="text-muted-foreground hidden items-center gap-1 text-xs font-medium sm:flex">
								{#if effectiveValue === 'floating'}
									<MonitorSpeakerIcon class="size-3" />
									{m.navigation_mode_floating()}
								{:else}
									<DockIcon class="size-3" />
									{m.navigation_mode_docked()}
								{/if}
							</span>
							<ArcaneButton
								action="base"
								tone="outline"
								size="sm"
								onclick={() => onLocalOverride(effectiveValue === 'floating' ? 'docked' : 'floating')}
								class="h-6 px-2 text-xs sm:h-7"
							>
								{m.override()}
							</ArcaneButton>
						</div>
					{/if}
				</div>
			</div>
		</div>

		<div class="bg-muted/30 rounded-md border p-2">
			<div class="flex items-center justify-between gap-2">
				<span class="text-muted-foreground text-xs font-medium">{m.current_state()}:</span>
				<div class="flex flex-wrap items-center justify-end gap-1">
					<span
						class={`flex items-center gap-1 text-xs font-medium ${effectiveValue === 'floating' ? 'text-blue-600 dark:text-blue-400' : 'text-purple-600 dark:text-purple-400'}`}
					>
						{#if effectiveValue === 'floating'}
							<MonitorSpeakerIcon class="size-3" />
							{m.navigation_mode_floating()}
						{:else}
							<DockIcon class="size-3" />
							{m.navigation_mode_docked()}
						{/if}
					</span>
					<span class="text-muted-foreground text-xs">
						({hasOverride ? m.local_override() : m.server_default()})
					</span>
				</div>
			</div>
		</div>
	</div>
</div>
