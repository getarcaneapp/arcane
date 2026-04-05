import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url }) => {
	const fromStack = url.searchParams.get('fromStack');
	const sourceStackName = fromStack ? decodeURIComponent(fromStack) : null;
	const templateId = url.searchParams.get('templateId');

	if (sourceStackName) {
		throw redirect(307, `/projects/swarm/${encodeURIComponent(sourceStackName)}`);
	}

	const templateSuffix = templateId ? `?templateId=${encodeURIComponent(templateId)}` : '';
	throw redirect(307, `/projects/swarm/new${templateSuffix}`);
};
