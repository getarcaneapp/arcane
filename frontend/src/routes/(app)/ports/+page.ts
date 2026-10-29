import { redirect } from '@sveltejs/kit';

import { withBase } from '#lib/utils/base-path.js';

import type { PageLoad } from './$types';

// Ports moved under /networks/ports; keep old bookmarks working.
export const load: PageLoad = ({ url }) => {
	redirect(301, withBase(`/networks/ports${url.search}`));
};
