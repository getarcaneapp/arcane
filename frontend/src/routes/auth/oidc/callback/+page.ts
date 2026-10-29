import { redirect } from '@sveltejs/kit';

import { withBase } from '#lib/utils/base-path.js';

import type { PageLoad } from './$types';

// Temporary redirect to handle OIDC callback uniformly

export const load: PageLoad = ({ url }) => {
	const searchParams = url.searchParams.toString();
	const redirectUrl = searchParams ? `/oidc/callback?${searchParams}` : '/oidc/callback';
	redirect(307, withBase(redirectUrl));
};
