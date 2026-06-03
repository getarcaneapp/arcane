<script lang="ts">
	import { volumeBrowserService } from '$lib/services/volume-browser-service';
	import { volumeBackupService } from '$lib/services/volume-backup-service';
	import { queryKeys } from '$lib/query/query-keys';
	import GenericFileBrowser, { type FileProvider } from './GenericFileBrowser.svelte';
	import { useQueryClient } from '@tanstack/svelte-query';

	let { volumeName }: { volumeName: string } = $props();
	const queryClient = useQueryClient();

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

<GenericFileBrowser {provider} rootLabel={volumeName} persistKey="volume-file-browser" />
