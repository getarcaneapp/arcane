import type { User } from '$lib/types/user.type';
import { writable } from 'svelte/store';
import { setLocale } from '$lib/utils/locale.util';
import { userService } from '$lib/services/user-service';

const userStore = writable<User | null>(null);

const setUser = async (user: User) => {
	if (user.locale) {
		await setLocale(user.locale, false);
	}
	userStore.set(user);
};

const clearUser = () => {
	userStore.set(null);
};

const reload = async () => {
	try {
		const user = await userService.getCurrentUser();
		await setUser(user);
	} catch (error) {
		console.error('Failed to reload user:', error);
	}
};

export default {
	subscribe: userStore.subscribe,
	setUser,
	clearUser,
	reload
};
