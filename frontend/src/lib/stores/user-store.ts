import type { User } from '$lib/types/user.type';
import { writable, get } from 'svelte/store';
import { setLocale } from '$lib/utils/locale.util';

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

const isAdmin = () => {
	const user = get(userStore);
	return !!user?.roles?.includes('admin');
};

export default {
	subscribe: userStore.subscribe,
	setUser,
	clearUser,
	isAdmin
};
