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
		contextMode,
		contextDir,
		remoteContext,
		onModeChange,
		onRemoteContextChange,
		onSelectContext
	}: {
		rootLabel: string;
		rootPath: string;
		contextMode: 'workspace' | 'remote';
		contextDir: string;
		remoteContext: string;
		onModeChange?: (mode: 'workspace' | 'remote') => void;
		onRemoteContextChange?: (value: string) => void;
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
		<div class="relative flex flex-col gap-3">
			<div class="flex items-center justify-between gap-3">
				<div class="flex items-center gap-3">
					<div
						class="flex size-9 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500/20 to-cyan-500/10 ring-1 ring-blue-400/20"
					>
						<FolderOpenIcon class="size-4 text-blue-400" />
					</div>
					<div>
						<h2 class="text-sm font-semibold tracking-tight">{m.build_context()}</h2>
						<p
							class="text-muted-foreground mt-0.5 max-w-[220px] truncate text-xs sm:max-w-[280px] lg:max-w-[360px]"
							title={contextDir}
						>
							{contextMode === 'remote' ? `${m.remote_source()}` : `${m.build_context()}:`}
							{contextDir}
						</p>
					</div>
				</div>
				<ArcaneButton action="base" tone="ghost" size="sm" onclick={() => goto('/settings/builds')} class="hover:bg-white/5">
					<SettingsIcon class="size-4" />
				</ArcaneButton>
			</div>

			<div class="flex items-center gap-2 rounded-lg border border-zinc-800/70 bg-zinc-950/50 p-1">
				<button
					type="button"
					class={`flex-1 rounded-md px-3 py-2 text-xs font-medium transition-colors ${
						contextMode === 'workspace'
							? 'bg-blue-500/12 text-blue-100 ring-1 ring-blue-400/20'
							: 'text-muted-foreground hover:text-foreground'
					}`}
					onclick={() => onModeChange?.('workspace')}
				>
					{m.build_context_mode_workspace()}
				</button>
				<button
					type="button"
					class={`flex-1 rounded-md px-3 py-2 text-xs font-medium transition-colors ${
						contextMode === 'remote'
							? 'bg-emerald-500/12 text-emerald-100 ring-1 ring-emerald-400/20'
							: 'text-muted-foreground hover:text-foreground'
					}`}
					onclick={() => onModeChange?.('remote')}
				>
					{m.build_context_mode_remote_git()}
				</button>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
		<div class="flex min-h-0 flex-1 flex-col p-4">
			{#if contextMode === 'workspace'}
				<BuildWorkspaceBrowser {provider} {rootLabel} {rootPath} onSelectContext={(path: string) => onSelectContext?.(path)} />
			{:else}
				<div
					class="flex h-full flex-col rounded-2xl border border-emerald-500/20 bg-gradient-to-br from-emerald-500/8 via-zinc-950 to-zinc-950 p-5"
				>
					<div class="max-w-xl space-y-2">
						<p class="text-sm font-semibold text-emerald-100">{m.build_remote_git_context_title()}</p>
						<p class="text-muted-foreground text-sm">
							{m.build_remote_git_context_description()}
						</p>
					</div>

					<div class="mt-5 space-y-3">
						<label for="remote-context-url" class="text-xs font-medium tracking-[0.12em] text-zinc-400 uppercase">
							{m.git_repository_url()}
						</label>
						<input
							id="remote-context-url"
							type="text"
							value={remoteContext}
							oninput={(event) => onRemoteContextChange?.((event.currentTarget as HTMLInputElement).value)}
							placeholder={m.build_remote_git_context_placeholder()}
							class="border-border bg-background/80 focus-visible:ring-ring w-full rounded-xl border px-4 py-3 text-sm transition outline-none focus-visible:ring-2"
							spellcheck="false"
							autocomplete="off"
						/>
					</div>

					<div class="mt-5 grid gap-3 text-xs text-zinc-400 sm:grid-cols-2">
						<div class="rounded-xl border border-zinc-800/70 bg-zinc-950/60 p-3">
							<div class="font-medium text-zinc-200">{m.build_remote_git_examples()}</div>
							<div class="mt-2 font-mono leading-5 break-all">
								https://github.com/owner/repo.git
								<br />
								https://github.com/owner/repo.git#main
								<br />
								https://github.com/owner/repo.git#main:docker/app
							</div>
						</div>
						<div class="rounded-xl border border-zinc-800/70 bg-zinc-950/60 p-3">
							<div class="font-medium text-zinc-200">{m.build_remote_git_credential_lookup_title()}</div>
							<div class="mt-2 leading-5">
								{m.build_remote_git_credential_lookup_description()}
							</div>
						</div>
					</div>
				</div>
			{/if}
		</div>
	</div>
</div>
