import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { userService } from '#lib/services/user-service.js';
import userStore from '#lib/stores/user-store.svelte.js';
import type { User, UserPreferences } from '#lib/types/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

let saves = Promise.resolve();
// The user as last saved, and the changes still waiting to save, in order.
let confirmed: User | undefined;
const pending: Partial<UserPreferences>[] = [];

/**
 * Applies preferences to the current user at once, then saves them in order, so a quick follow-up
 * change builds on this one. After each save the user is the saved one plus the changes still
 * waiting; a failed change is dropped and its error shown.
 */
export function saveUserPreferences(patch: Partial<UserPreferences>): Promise<void> {
	const current = userStore.current;
	if (!current) return Promise.resolve();
	if (pending.length === 0) confirmed = current;
	pending.push(patch);
	void userStore.setUser({ ...current, preferences: { ...current.preferences, ...patch } });
	// A save is dropped, store update included, once its user has signed out or another has signed in.
	const signedIn = () => userStore.current?.id === current.id;
	const save = saves.then(async () => {
		if (!signedIn()) {
			pending.shift();
			return;
		}
		const operationResult = await tryCatch(userService.updateMyProfile({ preferences: patch }));
		pending.shift();
		if (!signedIn()) return;
		if (operationResult.error !== null) {
			const error = operationResult.error;

			toast.error(error instanceof Error ? error.message : m.common_update_failed({ resource: m.account_preferences() }));
		} else {
			confirmed = operationResult.data;
		}
		const saved = confirmed ?? current;
		await userStore.setUser({ ...saved, preferences: Object.assign({}, saved.preferences, ...pending) });
	});
	// One failed save must not stop the ones queued after it.
	saves = save.catch(() => undefined);
	return save;
}
