import { redirect } from '@sveltejs/kit';

export const load = async ({ parent, url }) => {
	const data = await parent();

	if (data.user) {
		throw redirect(302, '/dashboard');
	}

	const redirectTo = url.searchParams.get('redirect') || '/dashboard';

	const error = url.searchParams.get('error');
	const errorMessage =
		url.searchParams.get('message') || url.searchParams.get('error_message') || url.searchParams.get('errorMessage');

	return {
		settings: data.settings,
		redirectTo,
		error,
		errorMessage,
		versionInformation: data.versionInformation
	};
};
