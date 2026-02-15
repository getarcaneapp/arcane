<script lang="ts">
	import { goto } from '$app/navigation';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import BuildWorkspaceBrowser from '../build-workspace-browser.svelte';
	import { buildWorkspaceService } from '$lib/services/build-workspace-service';
	import { m } from '$lib/paraglide/messages';
	import { FolderOpenIcon, SettingsIcon } from '$lib/icons';
	import type { FileProvider } from '$lib/components/file-browser';

	let {
		rootLabel,
		rootPath,
		contextDir,
		onSelectContext
	}: {
		rootLabel: string;
		rootPath: string;
		contextDir: string;
		onSelectContext?: (path: string) => void;
	} = $props();

	const provider: FileProvider = {
		list: (path: string) => buildWorkspaceService.listDirectory(path),
		mkdir: (path: string) => buildWorkspaceService.createDirectory(path),
		upload: (path: string, file: File) => buildWorkspaceService.uploadFile(path, file),
		delete: (path: string) => buildWorkspaceService.deleteFile(path),
		download: (path: string) => buildWorkspaceService.downloadFile(path),
		getContent: (path: string) => buildWorkspaceService.getFileContent(path)
	};
</script>

<div class="flex h-full flex-col">
	<div class="relative border-b border-zinc-800/50 bg-gradient-to-r from-zinc-900/50 to-transparent px-4 py-3">
		<div class="absolute inset-0 bg-gradient-to-br from-blue-500/5 via-transparent to-transparent"></div>
		<div class="relative flex items-center justify-between">
			<div class="flex items-center gap-3">
				<div
					class="flex size-9 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500/20 to-cyan-500/10 ring-1 ring-blue-400/20"
				>
					<FolderOpenIcon class="size-4 text-blue-400" />
				</div>
				<div>
					<h2 class="text-sm font-semibold tracking-tight">{m.build_workspace_files()}</h2>
					<p
						class="text-muted-foreground mt-0.5 max-w-[220px] truncate text-xs sm:max-w-[280px] lg:max-w-[360px]"
						title={contextDir}
					>
						{m.build_context()}: {contextDir}
					</p>
				</div>
			</div>
			<ArcaneButton action="base" tone="ghost" size="sm" onclick={() => goto('/settings/build')} class="hover:bg-white/5">
				<SettingsIcon class="size-4" />
			</ArcaneButton>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
		<div class="flex min-h-0 flex-1 flex-col p-4">
			<BuildWorkspaceBrowser {provider} {rootLabel} {rootPath} onSelectContext={(path: string) => onSelectContext?.(path)} />
		</div>
	</div>
</div>
