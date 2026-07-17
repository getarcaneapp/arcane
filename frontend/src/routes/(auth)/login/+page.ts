import { redirect } from '@sveltejs/kit';

export const load = async ({ parent, url }) => {
	const data = await parent();

	const rawRedirect = url.searchParams.get('redirect') || '/dashboard';
	// Guard against open redirects — only allow same-origin relative paths
	const redirectTo = rawRedirect.startsWith('/') && !rawRedirect.startsWith('//') ? rawRedirect : '/dashboard';

	if (data.user) {
		throw redirect(302, redirectTo);
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
