import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

// Temporary redirect to handle OIDC callback uniformly

export const load: PageLoad = ({ url }) => {
	const searchParams = url.searchParams.toString();
	const redirectUrl = searchParams ? `/oidc/callback?${searchParams}` : '/oidc/callback';
	redirect(307, redirectUrl);
};
