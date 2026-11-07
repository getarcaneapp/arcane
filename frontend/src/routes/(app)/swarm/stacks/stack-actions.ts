import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { confirmAndRun } from '#lib/utils/bulk-actions.js';
import { tryCatch } from '#lib/utils/try-catch.js';

type StackActionOptions = {
	name: string;
	environmentId: string;
	setLoading: (loading: boolean) => void;
	onSuccess: () => Promise<unknown>;
};

export async function bringUpStack({ name, environmentId, setLoading, onSuccess }: StackActionOptions): Promise<void> {
	setLoading(true);
	await handleApiResultWithCallbacks({
		result: await tryCatch(swarmService.upStack(name, environmentId)),
		message: m.failed_to_bring_up_stack({ name }),
		setLoadingState: setLoading,
		onSuccess: async () => {
			toast.success(m.stack_brought_up({ name }));
			await onSuccess();
		}
	});
}

export function bringDownStack({ name, environmentId, setLoading, onSuccess }: StackActionOptions): void {
	confirmAndRun({
		title: m.bring_down_stack(),
		message: m.bring_down_stack_confirm_message(),
		confirmLabel: m.common_down(),
		destructive: true,
		setLoading,
		run: () => swarmService.downStack(name, environmentId),
		failureMessage: m.failed_to_bring_down_stack({ name }),
		onSuccess: async () => {
			toast.success(m.stack_brought_down({ name }));
			await onSuccess();
		}
	});
}
