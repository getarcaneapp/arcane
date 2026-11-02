<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { DownloadIcon, EllipsisIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { ThemeDefinition } from '#lib/types/settings.js';
	import { downloadBlob } from '#lib/utils/browser-download.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

	let {
		theme,
		label,
		description,
		custom
	}: {
		theme: ThemeDefinition;
		label: string;
		description: string;
		custom: boolean;
	} = $props();

	function exportTheme() {
		const exported: ThemeDefinition = custom
			? theme
			: {
					id: `custom-${theme.id}-copy`,
					name: m.name_copy({ name: label }),
					description,
					light: theme.light,
					dark: theme.dark
				};
		downloadBlob(JSON.stringify(exported, null, 2), `${exported.id}.json`);
	}

	function deleteTheme() {
		openConfirmDialog({
			title: m.delete_name({ name: label }),
			message: m.common_delete_confirm({ resource: label }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: () => {
					const preferences = userStore.current?.preferences ?? {};
					void saveUserPreferences({
						customThemes: (preferences.customThemes ?? []).filter((item) => item.id !== theme.id),
						...(preferences.applicationTheme === theme.id ? { applicationTheme: 'default' } : {})
					});
				}
			}
		});
	}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{...props}
				action="base"
				tone="ghost"
				size="icon"
				class="size-6 p-0"
				icon={EllipsisIcon}
				showLabel={false}
				customLabel={m.common_more_actions()}
			/>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		<DropdownMenu.Group>
			<DropdownMenu.Item onclick={exportTheme}>
				<DownloadIcon class="size-4" />
				{m.images_export()}
			</DropdownMenu.Item>
			{#if custom}
				<DropdownMenu.Item variant="destructive" onclick={deleteTheme}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			{/if}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>
