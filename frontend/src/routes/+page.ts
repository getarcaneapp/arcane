import { redirect } from '@sveltejs/kit';

import { withBase } from '#lib/utils/base-path.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	return redirect(302, withBase('/login'));
};
