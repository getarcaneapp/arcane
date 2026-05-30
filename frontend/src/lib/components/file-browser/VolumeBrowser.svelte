<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { volumeBrowserService } from '$lib/services/volume-browser-service';
	import { volumeBackupService } from '$lib/services/volume-backup-service';
	import { queryKeys } from '$lib/query/query-keys';
	import GenericFileBrowser, { type FileProvider } from './GenericFileBrowser.svelte';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { StopIcon } from '$lib/icons';
	import { apiClient } from '$lib/services/api-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';

	let { volumeName }: { volumeName: string } = $props();
	const queryClient = useQueryClient();

	// Resolved once on mount so the page-unload beacon can fire synchronously.
	let envId: string | null = null;

	async function handleStopBrowsing() {
		try {
			await volumeBrowserService.stopBrowsing(volumeName);
			toast.success(m.volumes_browser_stop_success());
		} catch {
			// Best-effort; the backend idle reaper will clean up regardless.
			toast.error(m.volumes_browser_stop_error());
		}
	}

	// Hard tab-close / refresh: SvelteKit's onDestroy can't reliably complete an
	// async request during unload, so fire a keepalive beacon instead. Cookie auth
	// (credentials are same-origin) means sendBeacon carries the session.
	function handlePageHide() {
		if (!envId) return;
		const base = apiClient.defaults.baseURL || '/api';
		navigator.sendBeacon(`${base}/environments/${envId}/volumes/${volumeName}/browse/stop`);
	}
	// Referenced only via <svelte:window>; keep the checker from flagging it unused.
	$effect(() => void handlePageHide);

	onMount(() => {
		void environmentStore.getCurrentEnvironmentId().then((id) => (envId = id));
	});

	onDestroy(() => {
		// Fires on tab switch (component unmounts) and in-app navigation (incl. back
		// button) — the leaky path competitors miss. Best-effort; reaper is the backstop.
		void volumeBrowserService.stopBrowsing(volumeName).catch(() => {});
	});

	const provider: FileProvider = {
		list: (path) =>
			queryClient.fetchQuery({
				queryKey: queryKeys.volumes.list(volumeName, path),
				queryFn: () => volumeBrowserService.listDirectory(volumeName, path),
				staleTime: 0
			}),
		mkdir: async (path) => {
			const result = await volumeBrowserService.createDirectory(volumeName, path);
			await queryClient.invalidateQueries({ queryKey: queryKeys.volumes.listPrefix(volumeName) });
			return result;
		},
		upload: async (path, file) => {
			const result = await volumeBrowserService.uploadFile(volumeName, path, file);
			await queryClient.invalidateQueries({ queryKey: queryKeys.volumes.listPrefix(volumeName) });
			return result;
		},
		delete: async (path) => {
			const result = await volumeBrowserService.deleteFile(volumeName, path);
			await queryClient.invalidateQueries({ queryKey: queryKeys.volumes.listPrefix(volumeName) });
			return result;
		},
		download: (path) => volumeBrowserService.downloadFile(volumeName, path),
		getContent: (path) =>
			queryClient.fetchQuery({
				queryKey: queryKeys.volumes.content(volumeName, path),
				queryFn: () => volumeBrowserService.getFileContent(volumeName, path),
				staleTime: 0
			}),
		listBackups: async () => {
			const res = await queryClient.fetchQuery({
				queryKey: queryKeys.volumes.backups(volumeName),
				queryFn: () =>
					volumeBackupService.listBackups(volumeName, {
						pagination: { page: 1, limit: 200 },
						sort: { column: 'createdAt', direction: 'desc' }
					}),
				staleTime: 0
			});
			return res.data;
		},
		restoreFromBackup: async (backupId, path) => {
			const result = await volumeBackupService.restoreBackupFiles(volumeName, backupId, [path]);
			await queryClient.invalidateQueries({ queryKey: queryKeys.volumes.listPrefix(volumeName) });
			return result;
		},
		backupHasPath: (backupId, path) =>
			queryClient.fetchQuery({
				queryKey: queryKeys.volumes.backupHasPath(backupId, path),
				queryFn: () => volumeBackupService.backupHasPath(backupId, path),
				staleTime: 0
			})
	};
</script>

<svelte:window onpagehide={handlePageHide} />

<GenericFileBrowser {provider} rootLabel={volumeName} persistKey="volume-file-browser">
	{#snippet headerActions()}
		<ArcaneButton
			action="base"
			tone="outline"
			size="sm"
			onclick={handleStopBrowsing}
			icon={StopIcon}
			customLabel={m.volumes_browser_stop()}
		/>
	{/snippet}
</GenericFileBrowser>
