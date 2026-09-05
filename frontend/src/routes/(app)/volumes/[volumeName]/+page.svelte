<script lang="ts">
	// fallow-ignore-file code-duplication -- useUrlTab initialization is the hook's intended per-page integration surface
	import * as Card from '#lib/components/ui/card/index.js';
	import {
		VolumesIcon,
		ClockIcon,
		TagIcon,
		LayersIcon,
		InfoIcon,
		GlobeIcon,
		ContainersIcon,
		BoxIcon,
		FileTextIcon,
		AlertIcon
	} from '#lib/icons';
	import { goto } from '$app/navigation';
	import { Badge } from '#lib/components/ui/badge';
	import { formatDateTimeShort, truncateString } from '#lib/utils/formatting';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '#lib/utils/api';
	import { handleApiResultWithCallbacks } from '#lib/utils/api';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { m } from '#lib/paraglide/messages';
	import { volumeService } from '#lib/services/volume-service.js';
	import { volumeWorkspaceService } from '#lib/services/volume-workspace-service';
	import { type DetailAction } from '#lib/layouts';
	import TabbedPageLayout from '#lib/layouts/tabbed-page-layout.svelte';
	import BackupList from '../components/volume-backup-table.svelte';
	import settingsStore from '#lib/stores/config-store';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';
	import { DetailMetaStrip, DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail';
	import InUseStatus from '#lib/components/arcane-table/cells/in-use-status.svelte';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import CodePanel from '#lib/components/code-panel.svelte';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import { composeTreeSplitProps } from '#lib/utils/compose-flow';
	import type { VolumeWorkspaceFileChange, VolumeWorkspaceFileContent } from '#lib/types/volume-workspace';
	import {
		applyWorkspaceFileChangesForDisplay,
		buildWorkspaceMultipartUpdate,
		isWorkspaceFileSelectionUnder,
		joinWorkspaceFilePath,
		remapSelectedWorkspaceFileKey,
		remapWorkspaceFileRecord,
		removeWorkspaceFileRecord,
		readWorkspaceTextUpload,
		validateWorkspaceFileName,
		workspaceFileBasename,
		workspaceFileLanguage,
		workspaceFileParentPath,
		workspaceFilePathMatches
	} from '#lib/utils/workspace-files';
	import { volumeBackupService } from '#lib/services/volume-backup-service';
	import type { BackupEntry } from '#lib/types/shared';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import * as Select from '#lib/components/ui/select';
	import { Label } from '#lib/components/ui/label';
	import * as Alert from '#lib/components/ui/alert';
	import { Spinner } from '#lib/components/ui/spinner';
	import { bytes } from '#lib/utils/formatting';
	import { PersistedState } from 'runed';
	import { volumeWorkspaceReadOnlyMessage } from '../components/volume-workspace-utils';

	let { data } = $props();
	let volume = $derived(data.volume);
	let containersDetailed = $derived<{ id: string; name: string }[]>(data.containersDetailed ?? []);

	const backupVolumeName = $derived.by(() => $settingsStore?.backupVolumeName || 'arcane-backups');
	const isBackupVolume = $derived(volume?.name === backupVolumeName);

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canDeleteVolume = $derived(hasPermission('volumes:delete', currentEnvId));
	const canReadVolume = $derived(hasPermission('volumes:read', currentEnvId));
	const canUploadVolume = $derived(hasPermission('volumes:upload', currentEnvId));
	const canBackupVolume = $derived(hasPermission('volumes:backup', currentEnvId));
	const volumeWorkspaceMaxFileSizeMb = $derived($settingsStore?.volumeWorkspaceMaxFileSizeMb ?? 10);

	let isLoading = $state({ remove: false, save: false });
	const createdDate = $derived(volume.createdAt ? formatDateTimeShort(volume.createdAt) : m.common_unknown());

	const tabItems = $derived([
		{ value: 'overview', label: m.common_overview() },
		{ value: 'workspace', label: m.workspace() },
		{ value: 'backups', label: m.volumes_nav_backups() }
	]);
	const urlTab = useUrlTab({
		validTabs: () => tabItems.map((tab) => tab.value),
		defaultTab: () => 'overview'
	});
	const selectedTab = $derived(urlTab.value);
	const queryClient = useQueryClient();

	let workspaceRequested = $state(false);
	let workspaceFileChanges = $state<VolumeWorkspaceFileChange[]>([]);
	let workspaceFileContents = $state<Record<string, string>>({});
	let loadedWorkspaceFileContents = $state<Record<string, string>>({});
	let workspaceFileMetadata = $state<Record<string, VolumeWorkspaceFileContent>>({});
	let workspaceFileLoadErrors = $state<Record<string, string>>({});
	let workspaceFileLoading = $state<Record<string, boolean>>({});
	let workspaceRestoreRecords = $state<Record<string, true>>({});
	const workspaceFileLoadVersions = new Map<string, number>();
	let selectedWorkspaceFile = $state('');
	let openWorkspaceTabs = $state<string[]>([]);
	let workspaceTreePaneWidth = $state(420);
	type VolumeWorkspaceUIPrefs = { selectedFile: string; openTabs: string[] };
	let workspacePrefs: PersistedState<VolumeWorkspaceUIPrefs> | null = null;
	let lastWorkspacePrefsKey = $state('');

	let showWorkspaceRestore = $state(false);
	let workspaceRestorePath = $state('');
	let workspaceBackups = $state<BackupEntry[]>([]);
	let selectedWorkspaceBackupId = $state('');
	let loadingWorkspaceBackups = $state(false);
	let checkingWorkspaceBackup = $state(false);
	let workspaceBackupHasPath = $state<boolean | null>(null);
	let lastWorkspaceBackupCheck = '';

	const workspaceQuery = createQuery(() => ({
		queryKey: queryKeys.volumes.workspace(currentEnvId, volume.name),
		queryFn: () => volumeWorkspaceService.getWorkspace(volume.name, currentEnvId),
		enabled: workspaceRequested && canReadVolume,
		refetchOnMount: false
	}));

	const visibleWorkspaceFiles = $derived.by(() =>
		applyWorkspaceFileChangesForDisplay(workspaceQuery.data?.files ?? [], workspaceFileChanges).map((file) => {
			const modeType = file.mode?.[0];
			const metadata = workspaceFileMetadata[file.relativePath];
			const editable = metadata?.editable ?? file.editable;
			return {
				...file,
				editable,
				readOnlyReason: metadata?.readOnlyReason ?? file.readOnlyReason,
				locked:
					!file.isDirectory &&
					(editable === false || file.isSymlink === true || (!!modeType && modeType !== '-' && modeType !== 'd'))
			};
		})
	);
	const workspaceFilePaths = $derived.by(() => new Set(visibleWorkspaceFiles.map((file) => file.relativePath)));
	const changedWorkspaceTextPaths = $derived.by(() =>
		Object.keys(workspaceFileContents).filter(
			(relativePath) => workspaceFileContents[relativePath] !== loadedWorkspaceFileContents[relativePath]
		)
	);
	const hasWorkspaceChanges = $derived(workspaceFileChanges.length > 0 || changedWorkspaceTextPaths.length > 0);
	const canSaveWorkspace = $derived(
		hasWorkspaceChanges &&
			!isLoading.save &&
			workspaceFileChanges.every((change) => {
				switch (change.operation) {
					case 'create_file':
					case 'create_folder':
					case 'update_file':
						return canUploadVolume;
					case 'rename':
					case 'move':
						return canUploadVolume && canDeleteVolume;
					case 'delete':
						return canDeleteVolume;
					case 'restore_file':
						return canBackupVolume;
				}
			}) &&
			(changedWorkspaceTextPaths.length === 0 || canUploadVolume)
	);
	const validWorkspaceTabs = $derived(
		openWorkspaceTabs.filter((key) => {
			if (!key.startsWith('file:')) return false;
			const entry = visibleWorkspaceFiles.find((file) => file.relativePath === key.slice(5));
			return !!entry && !entry.isDirectory;
		})
	);
	const activeWorkspaceTab = $derived(
		validWorkspaceTabs.includes(selectedWorkspaceFile) ? selectedWorkspaceFile : (validWorkspaceTabs[0] ?? '')
	);
	const workspaceTabs = $derived(
		validWorkspaceTabs.map((key) => {
			const relativePath = key.slice(5);
			return {
				key,
				label: workspaceFileBasename(relativePath),
				title: relativePath,
				iconClass: 'text-muted-foreground',
				pending:
					changedWorkspaceTextPaths.includes(relativePath) ||
					visibleWorkspaceFiles.find((file) => file.relativePath === relativePath)?.pending === true
			};
		})
	);
	const selectedWorkspaceEntry = $derived.by(() => {
		if (!activeWorkspaceTab.startsWith('file:')) return undefined;
		return visibleWorkspaceFiles.find((file) => file.relativePath === activeWorkspaceTab.slice(5));
	});
	const selectedWorkspaceMetadata = $derived(
		activeWorkspaceTab.startsWith('file:') ? workspaceFileMetadata[activeWorkspaceTab.slice(5)] : undefined
	);

	$effect(() => {
		if (selectedTab === 'workspace') workspaceRequested = true;
	});

	$effect(() => {
		const key = `arcane.volume.workspace.ui:${currentEnvId}:${volume.name}`;
		if (lastWorkspacePrefsKey === key) return;
		lastWorkspacePrefsKey = key;
		workspacePrefs = new PersistedState<VolumeWorkspaceUIPrefs>(
			key,
			{ selectedFile: '', openTabs: [] },
			{
				storage: 'session',
				syncTabs: false
			}
		);
		selectedWorkspaceFile = workspacePrefs.current.selectedFile;
		openWorkspaceTabs = workspacePrefs.current.openTabs;
	});

	function persistWorkspacePrefs() {
		if (!workspacePrefs) return;
		workspacePrefs.current = { selectedFile: selectedWorkspaceFile, openTabs: openWorkspaceTabs };
	}

	function workspaceReadOnlyMessage(reason?: string): string {
		return volumeWorkspaceReadOnlyMessage(reason as VolumeWorkspaceFileContent['readOnlyReason'], volumeWorkspaceMaxFileSizeMb);
	}

	function closeWorkspaceTab(key: string) {
		const index = validWorkspaceTabs.indexOf(key);
		const remaining = validWorkspaceTabs.filter((tab) => tab !== key);
		openWorkspaceTabs = openWorkspaceTabs.filter((tab) => tab !== key);
		if (selectedWorkspaceFile === key) {
			selectedWorkspaceFile = remaining[Math.min(Math.max(index - 1, 0), remaining.length - 1)] ?? '';
		}
		persistWorkspacePrefs();
	}

	function openWorkspaceFile(key: string) {
		const relativePath = key.startsWith('file:') ? key.slice(5) : '';
		const entry = visibleWorkspaceFiles.find((file) => file.relativePath === relativePath);
		if (!entry) return;
		if (entry.isDirectory) {
			selectedWorkspaceFile = key;
			persistWorkspacePrefs();
			return;
		}
		if (!openWorkspaceTabs.includes(key)) openWorkspaceTabs = [...openWorkspaceTabs, key];
		selectedWorkspaceFile = key;
		persistWorkspacePrefs();
	}

	async function loadWorkspaceFile(relativePath: string) {
		if (!relativePath || workspaceFileMetadata[relativePath] || workspaceFileLoading[relativePath]) return;
		const entry = visibleWorkspaceFiles.find((file) => file.relativePath === relativePath);
		if (!entry || entry.isDirectory) return;
		if (workspaceRestoreRecords[relativePath]) {
			workspaceFileMetadata = {
				...workspaceFileMetadata,
				[relativePath]: {
					path: entry.path,
					relativePath,
					name: entry.name,
					size: entry.size,
					mimeType: '',
					editable: false,
					readOnlyReason: 'restore_pending'
				}
			};
			return;
		}
		if (entry.isSymlink) {
			workspaceFileMetadata = {
				...workspaceFileMetadata,
				[relativePath]: {
					path: entry.path,
					relativePath,
					name: entry.name,
					size: entry.size,
					mimeType: '',
					editable: false,
					readOnlyReason: 'symlink'
				}
			};
			return;
		}

		workspaceFileLoading = { ...workspaceFileLoading, [relativePath]: true };
		workspaceFileLoadErrors = removeWorkspaceFileRecord(workspaceFileLoadErrors, relativePath);
		const loadVersion = workspaceFileLoadVersions.get(relativePath) ?? 0;
		try {
			const file = await volumeWorkspaceService.getWorkspaceFile(volume.name, relativePath, currentEnvId);
			if (loadVersion !== (workspaceFileLoadVersions.get(relativePath) ?? 0)) return;
			workspaceFileMetadata = { ...workspaceFileMetadata, [relativePath]: file };
			if (file.editable) {
				const content = file.content ?? '';
				loadedWorkspaceFileContents = { ...loadedWorkspaceFileContents, [relativePath]: content };
				if (workspaceFileContents[relativePath] === undefined) {
					workspaceFileContents = { ...workspaceFileContents, [relativePath]: content };
				}
			}
		} catch (error) {
			if (loadVersion !== (workspaceFileLoadVersions.get(relativePath) ?? 0)) return;
			workspaceFileLoadErrors = {
				...workspaceFileLoadErrors,
				[relativePath]: error instanceof Error ? error.message : String(error)
			};
		} finally {
			if (loadVersion === (workspaceFileLoadVersions.get(relativePath) ?? 0)) {
				workspaceFileLoading = removeWorkspaceFileRecord(workspaceFileLoading, relativePath);
			}
		}
	}

	$effect(() => {
		if (!activeWorkspaceTab.startsWith('file:')) return;
		const relativePath = activeWorkspaceTab.slice(5);
		if (!workspaceFileMetadata[relativePath] && !workspaceFileLoadErrors[relativePath]) void loadWorkspaceFile(relativePath);
	});

	function createVolumeWorkspaceFile(parentPath: string, name: string) {
		const normalizedName = validateWorkspaceFileName(name);
		if (!normalizedName) return;
		const relativePath = joinWorkspaceFilePath(parentPath, normalizedName);
		if (workspaceFilePaths.has(relativePath)) {
			toast.error(m.workspace_file_duplicate_name());
			return;
		}
		workspaceFileChanges = [...workspaceFileChanges, { operation: 'create_file', relativePath }];
		workspaceFileContents = { ...workspaceFileContents, [relativePath]: '' };
		loadedWorkspaceFileContents = { ...loadedWorkspaceFileContents, [relativePath]: '' };
		workspaceFileMetadata = {
			...workspaceFileMetadata,
			[relativePath]: {
				path: `/${relativePath}`,
				relativePath,
				name: normalizedName,
				size: 0,
				mimeType: 'text/plain',
				content: '',
				editable: true
			}
		};
		openWorkspaceFile(`file:${relativePath}`);
	}

	function createVolumeWorkspaceFolder(parentPath: string, name: string) {
		const normalizedName = validateWorkspaceFileName(name);
		if (!normalizedName) return;
		const relativePath = joinWorkspaceFilePath(parentPath, normalizedName);
		if (workspaceFilePaths.has(relativePath)) {
			toast.error(m.workspace_file_duplicate_name());
			return;
		}
		workspaceFileChanges = [...workspaceFileChanges, { operation: 'create_folder', relativePath }];
		selectedWorkspaceFile = `file:${relativePath}`;
		persistWorkspacePrefs();
	}

	function remapVolumeWorkspaceState(oldPath: string, newPath: string) {
		workspaceFileContents = remapWorkspaceFileRecord(workspaceFileContents, oldPath, newPath);
		loadedWorkspaceFileContents = remapWorkspaceFileRecord(loadedWorkspaceFileContents, oldPath, newPath);
		workspaceFileMetadata = remapWorkspaceFileRecord(workspaceFileMetadata, oldPath, newPath);
		workspaceFileLoadErrors = remapWorkspaceFileRecord(workspaceFileLoadErrors, oldPath, newPath);
		workspaceFileLoading = remapWorkspaceFileRecord(workspaceFileLoading, oldPath, newPath);
		workspaceRestoreRecords = remapWorkspaceFileRecord(workspaceRestoreRecords, oldPath, newPath);
		openWorkspaceTabs = openWorkspaceTabs.map((tab) => remapSelectedWorkspaceFileKey(tab, oldPath, newPath) ?? tab);
		const remappedSelection = remapSelectedWorkspaceFileKey(selectedWorkspaceFile, oldPath, newPath);
		if (remappedSelection) selectedWorkspaceFile = remappedSelection;
		persistWorkspacePrefs();
	}

	function renameVolumeWorkspaceFile(relativePath: string, newName: string) {
		const normalizedName = validateWorkspaceFileName(newName);
		if (!normalizedName) return;
		const newPath = joinWorkspaceFilePath(workspaceFileParentPath(relativePath), normalizedName);
		if (newPath !== relativePath && workspaceFilePaths.has(newPath)) {
			toast.error(m.workspace_file_duplicate_name());
			return;
		}
		workspaceFileChanges = [...workspaceFileChanges, { operation: 'rename', relativePath, newName: normalizedName }];
		remapVolumeWorkspaceState(relativePath, newPath);
	}

	function moveVolumeWorkspaceFile(relativePath: string, newParentPath: string) {
		const entry = visibleWorkspaceFiles.find((file) => file.relativePath === relativePath);
		if (!entry || newParentPath === workspaceFileParentPath(relativePath)) return;
		if (entry.isDirectory && newParentPath && workspaceFilePathMatches(newParentPath, relativePath)) {
			toast.error(m.workspace_file_invalid_move_destination());
			return;
		}
		const newPath = joinWorkspaceFilePath(newParentPath, workspaceFileBasename(relativePath));
		if (workspaceFilePaths.has(newPath)) {
			toast.error(m.workspace_file_duplicate_name());
			return;
		}
		workspaceFileChanges = [...workspaceFileChanges, { operation: 'move', relativePath, newParentPath }];
		remapVolumeWorkspaceState(relativePath, newPath);
	}

	function removeVolumeWorkspaceState(relativePath: string) {
		workspaceFileContents = removeWorkspaceFileRecord(workspaceFileContents, relativePath);
		loadedWorkspaceFileContents = removeWorkspaceFileRecord(loadedWorkspaceFileContents, relativePath);
		workspaceFileMetadata = removeWorkspaceFileRecord(workspaceFileMetadata, relativePath);
		workspaceFileLoadErrors = removeWorkspaceFileRecord(workspaceFileLoadErrors, relativePath);
		workspaceFileLoading = removeWorkspaceFileRecord(workspaceFileLoading, relativePath);
		workspaceRestoreRecords = removeWorkspaceFileRecord(workspaceRestoreRecords, relativePath);
		openWorkspaceTabs = openWorkspaceTabs.filter((tab) => !isWorkspaceFileSelectionUnder(tab, relativePath));
		if (isWorkspaceFileSelectionUnder(selectedWorkspaceFile, relativePath)) selectedWorkspaceFile = openWorkspaceTabs[0] ?? '';
		persistWorkspacePrefs();
	}

	function deleteVolumeWorkspaceFile(relativePath: string) {
		const entry = visibleWorkspaceFiles.find((file) => file.relativePath === relativePath);
		if (!entry) return;
		workspaceFileChanges = [...workspaceFileChanges, { operation: 'delete', relativePath, recursive: entry.isDirectory }];
		removeVolumeWorkspaceState(relativePath);
	}

	async function stageVolumeUpload(parentPath: string, file: File, overwrite: boolean) {
		const relativePath = joinWorkspaceFilePath(parentPath, file.name);
		const existing = visibleWorkspaceFiles.find((entry) => entry.relativePath === relativePath);
		if (existing?.isDirectory) return;
		const upload = await readWorkspaceTextUpload(file, volumeWorkspaceMaxFileSizeMb);
		if (upload.error) return upload.error;
		const uploadedText = upload.content ?? '';
		workspaceFileChanges = [...workspaceFileChanges, { operation: overwrite ? 'update_file' : 'create_file', relativePath }];
		workspaceFileMetadata = {
			...workspaceFileMetadata,
			[relativePath]: {
				path: `/${relativePath}`,
				relativePath,
				name: file.name,
				size: file.size,
				mimeType: file.type || 'application/octet-stream',
				editable: true
			}
		};
		workspaceFileContents = { ...workspaceFileContents, [relativePath]: uploadedText };
		if (loadedWorkspaceFileContents[relativePath] === undefined) {
			loadedWorkspaceFileContents = { ...loadedWorkspaceFileContents, [relativePath]: '' };
		}
		openWorkspaceFile(`file:${relativePath}`);
	}

	async function uploadVolumeWorkspaceFiles(parentPath: string, files: File[]): Promise<string | void> {
		const invalidFile = files.find((file) => !validateWorkspaceFileName(file.name));
		if (invalidFile) return m.workspace_file_invalid_name();
		if (new Set(files.map((file) => file.name)).size !== files.length) return m.workspace_file_duplicate_name();
		const directoryCollision = files.find((file) =>
			visibleWorkspaceFiles.some(
				(entry) => entry.relativePath === joinWorkspaceFilePath(parentPath, file.name) && entry.isDirectory
			)
		);
		if (directoryCollision) return m.workspace_file_duplicate_name();

		const collisions = files.filter((file) => workspaceFilePaths.has(joinWorkspaceFilePath(parentPath, file.name)));
		for (const file of files.filter((candidate) => !collisions.includes(candidate))) {
			const error = await stageVolumeUpload(parentPath, file, false);
			if (error) return error;
		}
		if (collisions.length > 0) {
			openConfirmDialog({
				title: m.volumes_workspace_upload_collision_title(),
				message: m.volumes_workspace_upload_collision_message({
					paths: collisions.map((file) => joinWorkspaceFilePath(parentPath, file.name)).join('\n')
				}),
				confirm: {
					label: m.upload(),
					action: async () => {
						for (const file of collisions) {
							const error = await stageVolumeUpload(parentPath, file, true);
							if (error) toast.error(error);
						}
					}
				}
			});
		}
	}

	function clearVolumeWorkspaceDrafts() {
		for (const relativePath of Object.keys(workspaceFileLoading)) expireWorkspaceFileLoad(relativePath);
		workspaceFileChanges = [];
		workspaceFileContents = {};
		loadedWorkspaceFileContents = {};
		workspaceFileMetadata = {};
		workspaceFileLoadErrors = {};
		workspaceFileLoading = {};
		workspaceRestoreRecords = {};
	}

	function expireWorkspaceFileLoad(relativePath: string) {
		workspaceFileLoadVersions.set(relativePath, (workspaceFileLoadVersions.get(relativePath) ?? 0) + 1);
	}

	async function refreshRestoredVolumeWorkspace() {
		await queryClient.cancelQueries({ queryKey: queryKeys.volumes.workspace(currentEnvId, volume.name) });
		clearVolumeWorkspaceDrafts();
		await workspaceQuery.refetch();
	}

	async function handleSaveVolumeWorkspace() {
		if (!workspaceQuery.data || !canSaveWorkspace) return;
		const update = buildWorkspaceMultipartUpdate(workspaceFileChanges, workspaceFileContents, loadedWorkspaceFileContents);
		handleApiResultWithCallbacks({
			result: await tryCatch(
				volumeWorkspaceService.updateWorkspace(
					volume.name,
					{
						fileTreeRevision: workspaceQuery.data.fileTreeRevision,
						fileChanges: update.fileChanges
					},
					update.files,
					currentEnvId
				)
			),
			message: m.common_save_failed(),
			setLoadingState: (value) => (isLoading.save = value),
			onSuccess: async (workspace) => {
				queryClient.setQueryData(queryKeys.volumes.workspace(currentEnvId, volume.name), workspace);
				clearVolumeWorkspaceDrafts();
				toast.success(m.volumes_workspace_save_success(), activityToastOptions(workspace.activityId));
			}
		});
	}

	async function openWorkspaceRestoreDialog(relativePath: string) {
		workspaceRestorePath = relativePath;
		workspaceBackups = [];
		selectedWorkspaceBackupId = '';
		workspaceBackupHasPath = null;
		lastWorkspaceBackupCheck = '';
		showWorkspaceRestore = true;
		loadingWorkspaceBackups = true;
		try {
			const response = await volumeBackupService.listBackups(volume.name, { pagination: { page: 1, limit: 100 } });
			workspaceBackups = response.data;
			selectedWorkspaceBackupId = response.data[0]?.id ?? '';
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_failed());
		} finally {
			loadingWorkspaceBackups = false;
		}
	}

	async function checkWorkspaceBackupPath(backupId: string, relativePath: string) {
		const key = `${backupId}:${relativePath}`;
		if (!backupId || !relativePath || key === lastWorkspaceBackupCheck) return;
		lastWorkspaceBackupCheck = key;
		checkingWorkspaceBackup = true;
		workspaceBackupHasPath = null;
		try {
			workspaceBackupHasPath = await volumeBackupService.backupHasPath(backupId, `/${relativePath}`);
		} catch (error) {
			workspaceBackupHasPath = null;
			toast.error(error instanceof Error ? error.message : m.common_failed());
		} finally {
			checkingWorkspaceBackup = false;
		}
	}

	$effect(() => {
		if (showWorkspaceRestore && selectedWorkspaceBackupId && workspaceRestorePath) {
			void checkWorkspaceBackupPath(selectedWorkspaceBackupId, workspaceRestorePath);
		}
	});

	function stageWorkspaceRestore() {
		if (!workspaceRestorePath || !selectedWorkspaceBackupId || workspaceBackupHasPath !== true) return;
		workspaceFileChanges = [
			...workspaceFileChanges,
			{
				operation: 'restore_file',
				relativePath: workspaceRestorePath,
				backupId: selectedWorkspaceBackupId
			}
		];
		removeVolumeWorkspaceState(workspaceRestorePath);
		workspaceRestoreRecords = { ...workspaceRestoreRecords, [workspaceRestorePath]: true };
		showWorkspaceRestore = false;
		toast.success(m.volumes_workspace_restore_staged());
	}

	function formatWorkspaceBackupLabel(backup: BackupEntry): string {
		return `${backup.id} · ${bytes.format(backup.size, { unitSeparator: ' ' }) ?? '-'}`;
	}

	async function downloadVolumeWorkspaceFile(relativePath: string) {
		try {
			await volumeWorkspaceService.downloadWorkspaceFile(volume.name, relativePath, currentEnvId);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_download_error());
		}
	}

	async function handleRemoveVolumeConfirm(volumeName: string) {
		const safeName = volumeName?.trim() || m.common_unknown();
		if (safeName === backupVolumeName) return;
		const message = volume.inUse
			? `${m.volumes_remove_confirm_message({ name: safeName })}\n\n${m.volumes_remove_in_use_warning()}`
			: m.volumes_remove_confirm_message({ name: safeName });

		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_volume() }),
			message,
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(volumeService.deleteVolume(safeName)),
						message: m.volumes_remove_failed({ name: safeName }),
						setLoadingState: (value) => (isLoading.remove = value),
						onSuccess: async (data) => {
							toast.success(m.volumes_remove_success({ name: safeName }), activityToastOptions(extractActivityId(data)));
							goto('/volumes');
						}
					});
				}
			}
		});
	}

	const actions: DetailAction[] = $derived.by(() => {
		const items: DetailAction[] = [];
		if (hasWorkspaceChanges) {
			items.push({
				id: 'save-workspace',
				action: 'save',
				label: m.common_save_changes(),
				loading: isLoading.save,
				disabled: !canSaveWorkspace,
				onclick: handleSaveVolumeWorkspace
			});
		}
		if (canDeleteVolume) {
			items.push({
				id: 'remove',
				action: 'remove',
				label: m.common_remove(),
				loading: isLoading.remove,
				disabled: isLoading.remove || isBackupVolume,
				onclick: () => handleRemoveVolumeConfirm(volume.name)
			});
		}
		return items;
	});

	function onTabChange(value: string) {
		urlTab.select(value);
	}
</script>

{#if volume}
	<TabbedPageLayout
		environmentScoped
		backUrl="/volumes"
		backLabel={m.resource_volumes_cap()}
		{tabItems}
		{selectedTab}
		{onTabChange}
	>
		{#snippet headerInfo()}
			<div class="flex flex-col gap-1">
				<h1 class="text-2xl font-semibold tracking-tight break-all sm:text-3xl">{volume.name}</h1>
				<div class="flex flex-wrap items-center gap-2 pt-1">
					<InUseStatus inUse={volume.inUse} />
					{#if volume.driver}
						<Badge variant="blue" minWidth="20">{volume.driver}</Badge>
					{/if}
					{#if volume.scope}
						<Badge variant="purple" minWidth="20">{volume.scope}</Badge>
					{/if}
				</div>
			</div>
		{/snippet}

		{#snippet headerActions()}
			<div class="flex items-center gap-2">
				{#each actions as act (act.id)}
					<ArcaneButton
						action={act.action}
						customLabel={act.label}
						loading={act.loading}
						disabled={act.disabled}
						onclick={act.onclick}
					/>
				{/each}
			</div>
		{/snippet}

		{#snippet tabContent(tab)}
			<div class="space-y-6">
				{#if tab === 'overview'}
					<DetailMetaStrip
						items={[
							{ icon: VolumesIcon, label: m.common_driver(), value: volume.driver ?? m.common_unknown() },
							{ icon: GlobeIcon, label: m.common_scope(), value: volume.scope ?? m.common_unknown() },
							{ icon: ClockIcon, value: createdDate },
							{ icon: LayersIcon, label: m.common_mountpoint(), value: volume.mountpoint ?? m.common_unknown(), mono: true }
						]}
					/>

					<DetailSection title={m.volumes_containers_using_title()} icon={ContainersIcon}>
						{#if containersDetailed.length > 0}
							<div class="divide-y rounded-lg border">
								{#each containersDetailed as c (c.id)}
									<div class="flex flex-col p-3 sm:flex-row sm:items-center">
										<div class="mb-2 w-full font-medium break-all sm:mb-0 sm:w-1/3">
											<a href="/containers/{c.id}" class="flex items-center text-primary hover:underline">
												<ContainersIcon class="mr-1.5 size-3.5 text-muted-foreground" />
												{c.name}
											</a>
										</div>
										<div class="w-full pl-0 sm:w-2/3 sm:pl-4">
											<code
												class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
												title={m.common_click_to_select()}
											>
												{truncateString(c.id, 48)}
											</code>
										</div>
									</div>
								{/each}
							</div>
						{:else if volume.containers && volume.containers.length > 0}
							<!-- Fallback to IDs if names not resolved -->
							<div class="divide-y rounded-lg border">
								{#each volume.containers as id (id)}
									<div class="flex items-center justify-between gap-3 p-3">
										<code class="font-mono text-sm break-all">{truncateString(id, 48)}</code>
										<a href={`/containers/${id}`} class="text-sm text-primary hover:underline">{m.common_view()}</a>
									</div>
								{/each}
							</div>
						{:else}
							<div class="text-muted-foreground">{m.volumes_no_containers_using()}</div>
						{/if}
					</DetailSection>

					{#if volume.labels && Object.keys(volume.labels).length > 0}
						<DetailSection title={m.common_labels()} icon={TagIcon}>
							<KeyValueGrid>
								{#each Object.entries(volume.labels) as [key, value] (key)}
									<KeyValueCard label={key} valueTitle={m.common_click_to_select()}>{value}</KeyValueCard>
								{/each}
							</KeyValueGrid>
						</DetailSection>
					{/if}

					{#if volume.options && Object.keys(volume.options).length > 0}
						<DetailSection title={m.common_driver_options()} icon={VolumesIcon}>
							<KeyValueGrid>
								{#each Object.entries(volume.options) as [key, value] (key)}
									<KeyValueCard label={key} valueTitle={m.common_click_to_select()}>{value}</KeyValueCard>
								{/each}
							</KeyValueGrid>
						</DetailSection>
					{/if}

					{#if (!volume.labels || Object.keys(volume.labels).length === 0) && (!volume.options || Object.keys(volume.options).length === 0)}
						<p class="border-t pt-6 text-center text-muted-foreground">{m.volumes_no_labels_or_options()}</p>
					{/if}
				{:else if tab === 'workspace'}
					{#if !canReadVolume}
						<div class="flex min-h-96 items-center justify-center rounded-lg border text-sm text-muted-foreground">
							{m.common_access_denied()}
						</div>
					{:else if workspaceQuery.isPending}
						<div class="flex min-h-96 items-center justify-center rounded-lg border text-muted-foreground">
							<Spinner class="size-6" />
						</div>
					{:else if workspaceQuery.error}
						<div class="flex min-h-96 items-center justify-center rounded-lg border px-4 text-sm text-destructive">
							{workspaceQuery.error instanceof Error ? workspaceQuery.error.message : m.common_failed()}
						</div>
					{:else}
						<div class="space-y-3">
							{#if workspaceQuery.data?.fileTreeTruncated}
								<Alert.Root>
									<AlertIcon class="size-4" />
									<Alert.Description>{m.volumes_workspace_truncated()}</Alert.Description>
								</Alert.Root>
							{/if}
							<div
								class="flex h-[calc(100vh-15rem)] min-h-[32rem] flex-col overflow-hidden rounded-lg border border-border bg-card"
							>
								<ResizableSplit
									class="h-full min-h-0 flex-1"
									{...composeTreeSplitProps}
									bind:size={workspaceTreePaneWidth}
									ariaLabel={m.compose_editor_resize_files_panel()}
									persistKey={`arcane.volume.workspace.split:${currentEnvId}:${volume.name}`}
								>
									{#snippet first()}
										<WorkspaceFileTreePanel
											title={m.volumes_workspace_files()}
											leadingRows={[]}
											entries={visibleWorkspaceFiles}
											selectedFile={selectedWorkspaceFile}
											disabled={isLoading.save}
											emptyMessage={m.volumes_workspace_files_empty()}
											uploadDescription={m.volumes_workspace_upload_description()}
											rootDestinationLabel={m.volumes_workspace_root_destination()}
											rootPathMessage={m.volumes_workspace_root_path()}
											deleteConfirmMessage={(name) => m.volumes_workspace_delete_confirm({ name })}
											lockedLabel={m.volumes_workspace_locked_file()}
											onSelect={openWorkspaceFile}
											onCreateFile={canUploadVolume ? createVolumeWorkspaceFile : undefined}
											onCreateFolder={canUploadVolume ? createVolumeWorkspaceFolder : undefined}
											onUpload={canUploadVolume ? uploadVolumeWorkspaceFiles : undefined}
											multipleUploads={true}
											allowUploadOverwrite={true}
											validateName={validateWorkspaceFileName}
											onRename={canUploadVolume && canDeleteVolume ? renameVolumeWorkspaceFile : undefined}
											onMove={canUploadVolume && canDeleteVolume ? moveVolumeWorkspaceFile : undefined}
											onDelete={canDeleteVolume ? deleteVolumeWorkspaceFile : undefined}
											onDownload={downloadVolumeWorkspaceFile}
											onRestore={canBackupVolume ? openWorkspaceRestoreDialog : undefined}
										/>
									{/snippet}

									{#snippet second()}
										<div class="flex h-full min-h-0 flex-1 flex-col">
											{#if workspaceTabs.length > 0}
												<EditorTabStrip
													tabs={workspaceTabs}
													activeKey={activeWorkspaceTab}
													onSelect={openWorkspaceFile}
													onClose={closeWorkspaceTab}
												/>
											{/if}
											<div class="flex min-h-0 flex-1 flex-col">
												{#if !activeWorkspaceTab}
													<div class="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
														{m.volumes_workspace_select_file()}
													</div>
												{:else}
													{@const relativePath = activeWorkspaceTab.slice(5)}
													{#if workspaceFileLoadErrors[relativePath]}
														<div class="flex h-full items-center justify-center px-4 text-sm text-destructive">
															{workspaceFileLoadErrors[relativePath]}
														</div>
													{:else if workspaceFileLoading[relativePath] || !selectedWorkspaceMetadata}
														<div class="flex h-full items-center justify-center text-muted-foreground">
															<Spinner class="size-5" />
														</div>
													{:else if selectedWorkspaceMetadata.editable && workspaceFileContents[relativePath] !== undefined}
														<CodePanel
															variant="plain"
															open={true}
															title={relativePath}
															language={workspaceFileLanguage(relativePath)}
															validationMode="none"
															bind:value={workspaceFileContents[relativePath]}
															readOnly={!canUploadVolume}
															fileId={`volume:${currentEnvId}:${volume.name}:${relativePath}`}
															originalValue={loadedWorkspaceFileContents[relativePath] ?? ''}
															enableDiff={true}
														/>
													{:else}
														<div class="flex h-full items-center justify-center p-6">
															<Card.Root class="w-full max-w-xl">
																<Card.Header icon={FileTextIcon}>
																	<div class="min-w-0">
																		<Card.Title>{workspaceFileBasename(relativePath)}</Card.Title>
																		<Card.Description
																			>{workspaceReadOnlyMessage(selectedWorkspaceMetadata.readOnlyReason)}</Card.Description
																		>
																	</div>
																</Card.Header>
																<Card.Content class="space-y-2 text-sm text-muted-foreground">
																	<p>{m.common_size()}: {bytes.format(selectedWorkspaceMetadata.size, { unitSeparator: ' ' })}</p>
																	{#if selectedWorkspaceMetadata.mimeType}<p>
																			{m.common_type()}: {selectedWorkspaceMetadata.mimeType}
																		</p>{/if}
																	{#if selectedWorkspaceEntry?.linkTarget}<p>
																			{m.volumes_symlink_target_tooltip({ target: selectedWorkspaceEntry.linkTarget })}
																		</p>{/if}
																</Card.Content>
															</Card.Root>
														</div>
													{/if}
												{/if}
											</div>
										</div>
									{/snippet}
								</ResizableSplit>
							</div>
						</div>
					{/if}
				{:else if tab === 'backups'}
					<BackupList volumeName={volume.name} {hasWorkspaceChanges} onWorkspaceRestored={refreshRestoredVolumeWorkspace} />
				{/if}
			</div>
		{/snippet}
	</TabbedPageLayout>
{:else}
	<div class="flex flex-col items-center justify-center px-4 py-16 text-center">
		<div class="mb-4 rounded-full bg-muted/30 p-4">
			<BoxIcon class="size-10 text-muted-foreground opacity-70" />
		</div>
		<h2 class="mb-2 text-xl font-medium">{m.common_not_found_title({ resource: m.resource_volumes_cap() })}</h2>
		<p class="mb-6 text-muted-foreground">
			{m.common_not_found_description({ resource: m.resource_volumes_cap().toLowerCase() })}
		</p>

		<ArcaneButton
			action="cancel"
			customLabel={m.common_back_to({ resource: m.resource_volumes_cap() })}
			onclick={() => goto('/volumes')}
			size="sm"
		/>
	</div>
{/if}

<ResponsiveDialog
	open={showWorkspaceRestore}
	onOpenChange={(open) => (showWorkspaceRestore = open)}
	title={m.volumes_workspace_restore()}
	description={m.volumes_workspace_backup_restore_desc()}
	contentClass="sm:max-w-[520px]"
>
	{#snippet children()}
		<div class="space-y-4 py-2">
			<Alert.Root class="py-2 [&>svg]:top-2">
				<InfoIcon class="size-4" />
				<Alert.Description class="text-xs">{m.volumes_backup_safety_info()}</Alert.Description>
			</Alert.Root>
			{#if workspaceRestorePath}
				<code class="block rounded bg-muted/40 px-2 py-1 font-mono text-xs break-all">/{workspaceRestorePath}</code>
			{/if}
			{#if loadingWorkspaceBackups}
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					{m.common_loading()}
				</div>
			{:else if workspaceBackups.length === 0}
				<div class="text-sm text-muted-foreground">{m.volumes_workspace_no_backups()}</div>
			{:else}
				<div class="space-y-2">
					<Label for="workspace-restore-backup">{m.volumes_workspace_backup()}</Label>
					<Select.Root
						type="single"
						value={selectedWorkspaceBackupId}
						onValueChange={(value) => {
							selectedWorkspaceBackupId = value;
							workspaceBackupHasPath = null;
							lastWorkspaceBackupCheck = '';
						}}
					>
						<Select.Trigger id="workspace-restore-backup" class="h-10 w-full overflow-hidden">
							<span class="min-w-0 flex-1 truncate">
								{workspaceBackups.find((backup) => backup.id === selectedWorkspaceBackupId)
									? formatWorkspaceBackupLabel(workspaceBackups.find((backup) => backup.id === selectedWorkspaceBackupId)!)
									: m.volumes_workspace_backup()}
							</span>
						</Select.Trigger>
						<Select.Content>
							{#each workspaceBackups as backup (backup.id)}
								<Select.Item value={backup.id}>{formatWorkspaceBackupLabel(backup)}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
				{#if checkingWorkspaceBackup}
					<div class="flex items-center gap-2 text-xs text-muted-foreground">
						<Spinner class="size-3" />
						{m.common_loading()}
					</div>
				{:else if workspaceBackupHasPath === false}
					<div class="text-xs text-destructive">{m.volumes_workspace_backup_missing_file()}</div>
				{/if}
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (showWorkspaceRestore = false)} />
		<ArcaneButton
			action="confirm"
			customLabel={m.volumes_workspace_restore_file()}
			onclick={stageWorkspaceRestore}
			disabled={loadingWorkspaceBackups || checkingWorkspaceBackup || workspaceBackupHasPath !== true}
		/>
	{/snippet}
</ResponsiveDialog>
