import { m } from '#lib/paraglide/messages.js';
import type { VolumeWorkspaceFileContent } from '#lib/types/ui/volume.js';
import { workspaceReadOnlyMessage } from '#lib/utils/workspace-files.js';

export function volumeWorkspaceReadOnlyMessage(
	reason: VolumeWorkspaceFileContent['readOnlyReason'],
	maxFileSizeMb: number
): string {
	if (reason === 'restore_pending') return m.volumes_workspace_restore_readonly();
	return workspaceReadOnlyMessage(reason, maxFileSizeMb);
}
