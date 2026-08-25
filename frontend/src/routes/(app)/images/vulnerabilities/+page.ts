import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

// Vulnerabilities moved to the top-level security section; this route exists so
// old links and bookmarks keep working.
export const load: PageLoad = async ({ url }) => {
	redirect(302, `/security${url.search}`);
};
