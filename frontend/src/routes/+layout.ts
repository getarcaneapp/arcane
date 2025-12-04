import { environmentStore } from '$lib/stores/environment.store.svelte';
import versionService from '$lib/services/version-service';
import { tryCatch } from '$lib/utils/try-catch';
import userStore from '$lib/stores/user-store';
import settingsStore from '$lib/stores/config-store';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { AppVersionInformation } from '$lib/types/application-configuration';
import { userService } from '$lib/services/user-service';
import { settingsService } from '$lib/services/settings-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';

export const ssr = false;

export const load = async () => {
	const settingsPromise = settingsService.getSettings().catch((e) => {
		console.error('Error fetching settings:', e);
		return settingsService.getPublicSettings().catch(() => null);
	});

	const environmentRequestOptions: SearchPaginationSortRequest = {
		pagination: {
			page: 1,
			limit: 1000
		}
	};

	const user = await userService.getCurrentUser().catch(() => null);

	if (user) {
		const environments = await tryCatch(environmentManagementService.getEnvironments(environmentRequestOptions));
		if (!environments.error) {
			await environmentStore.initialize(environments.data.data);
		} else {
			await environmentStore.initialize([]);
		}
	} else {
		await environmentStore.initialize([]);
	}

	let versionInformation: AppVersionInformation = {
		currentVersion: versionService.getCurrentVersion(),
		displayVersion: versionService.getCurrentVersion(),
		revision: 'unknown',
		shortRevision: 'unknown',
		goVersion: 'unknown',
		isSemverVersion: false
	};

	try {
		const info = await versionService.getVersionInformation();
		versionInformation = {
			currentVersion: info.currentVersion,
			currentTag: info.currentTag,
			currentDigest: info.currentDigest,
			displayVersion: info.displayVersion,
			revision: info.revision,
			shortRevision: info.shortRevision || (info.revision?.slice(0, 8) ?? 'unknown'),
			goVersion: info.goVersion || 'unknown',
			buildTime: info.buildTime,
			isSemverVersion: info.isSemverVersion,
			newestVersion: info.newestVersion,
			newestDigest: info.newestDigest,
			updateAvailable: info.updateAvailable,
			releaseUrl: info.releaseUrl
		};
	} catch {}

	const settings = await settingsPromise;

	if (user) {
		await userStore.setUser(user);
	}

	if (settings) {
		settingsStore.set(settings);
	}

	return {
		user,
		settings,
		versionInformation
	};
};
