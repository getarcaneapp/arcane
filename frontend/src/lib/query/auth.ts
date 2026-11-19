import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { authService } from '#lib/services/auth-service.js';
import { passkeyService } from '#lib/services/passkey-service.js';
import { toAppVersionInformation } from '#lib/services/version-service.js';

export function sessionBootstrapQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.sessionBootstrap(),
		queryFn: async () => {
			const loaded = await authService.getSessionBootstrap();
			return { ...loaded, version: toAppVersionInformation(loaded.version) };
		}
	});
}

export function autoLoginConfigQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.autoLoginConfig(),
		queryFn: () => authService.getAutoLoginConfig()
	});
}

export function autoLoginAttemptQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.autoLoginAttempt(),
		queryFn: () => authService.attemptAutoLogin()
	});
}

export function passkeyLoginAvailabilityQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.passkeyLoginAvailability(),
		queryFn: () => passkeyService.getLoginAvailability()
	});
}

export function passkeysQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.passkeys(),
		queryFn: () => passkeyService.listMine()
	});
}

export function passkeyCapabilitiesQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.passkeyCapabilities(),
		queryFn: () => passkeyService.getCapabilities()
	});
}

export function mfaStatusQuery() {
	return queryOptions({
		queryKey: queryKeys.auth.mfaStatus(),
		queryFn: () => passkeyService.getMFAStatus()
	});
}
