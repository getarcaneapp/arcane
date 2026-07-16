import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = ({ url }) => {
	redirect(308, `/workloads/containers${url.search}`);
};
