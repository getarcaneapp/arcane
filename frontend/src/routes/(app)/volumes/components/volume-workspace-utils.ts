import { m } from '#lib/paraglide/messages';
import type { VolumeWorkspaceFileContent } from '#lib/types/volume-workspace';
import { workspaceReadOnlyMessage } from '#lib/utils/workspace-files';

export function volumeWorkspaceReadOnlyMessage(
	reason: VolumeWorkspaceFileContent['readOnlyReason'],
	maxFileSizeMb: number
): string {
	if (reason === 'restore_pending') return m.volumes_workspace_restore_readonly();
	return workspaceReadOnlyMessage(reason, maxFileSizeMb);
}
