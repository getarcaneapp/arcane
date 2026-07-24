import { redirect } from '@sveltejs/kit';
import { getEffectiveLandingPage } from '#lib/utils/navigation';

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

	const error = url.searchParams.get('error');
	const errorMessage =
		url.searchParams.get('message') || url.searchParams.get('error_message') || url.searchParams.get('errorMessage');

	return {
		settings: data.settings,
		redirectTo,
		error,
		// fallow-ignore-next-line unused-load-data-key -- rendered in the sibling page's login error alerts
		errorMessage,
		// fallow-ignore-next-line unused-load-data-key -- rendered in the sibling page's desktop and mobile version labels
		versionInformation: data.versionInformation
	};
};
