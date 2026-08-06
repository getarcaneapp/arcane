import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

// Ports moved under /networks/ports; keep old bookmarks working.
export const load: PageLoad = ({ url }) => {
	redirect(301, `/networks/ports${url.search}`);
};
