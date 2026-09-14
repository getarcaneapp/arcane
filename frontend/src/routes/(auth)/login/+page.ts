import { tryCatch } from '#lib/utils/try-catch.js';
import { redirect } from '@sveltejs/kit';
import { browser } from '$app/env';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { passkeyService } from '#lib/services/passkey-service.js';

export const load = async ({ parent, url }) => {
	const data = await parent();

	// Only an explicit `redirect` param produces a target here. The account-level
	// landing page is a per-user preference, so it can only be resolved once the
	// user is signed in — the post-login handlers fall back to it themselves.
	const rawRedirect = url.searchParams.get('redirect');
	// Guard against open redirects — only allow same-origin relative paths
	const redirectTo = rawRedirect?.startsWith('/') && !rawRedirect.startsWith('//') ? rawRedirect : '';

	if (data.user) {
		throw redirect(302, redirectTo || getEffectiveLandingPage());
	}

	const passkeyAvailability = await tryCatch(passkeyService.getLoginAvailability()).then((result) =>
		result.error ? null : result.data
	);

	const error = url.searchParams.get('error');
	const storedErrorMessage = browser ? sessionStorage.getItem('oidc_login_error') : null;
	if (storedErrorMessage) sessionStorage.removeItem('oidc_login_error');
	const errorMessage =
		url.searchParams.get('message') ||
		url.searchParams.get('error_message') ||
		url.searchParams.get('errorMessage') ||
		storedErrorMessage;

	return {
		passkeyLoginAvailable: passkeyAvailability?.available === true,
		settings: data.settings,
		redirectTo,
		error,
		errorMessage,
		versionInformation: data.versionInformation
	};
};
