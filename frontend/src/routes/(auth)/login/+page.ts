import { redirect } from '@sveltejs/kit';

export const load = async ({ parent, url }) => {
	const data = await parent();

	if (data.user) {
		throw redirect(302, '/dashboard');
	}

	const redirectTo = url.searchParams.get('redirect') || '/dashboard';

	const error = url.searchParams.get('error');

	return {
		settings: data.settings,
		redirectTo,
		error,
		versionInformation: data.versionInformation
	};
};
