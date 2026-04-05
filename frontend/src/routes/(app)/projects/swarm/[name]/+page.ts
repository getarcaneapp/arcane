import type { PageLoad } from './$types';
import { loadExistingSwarmProjectEditorData } from '../editor-data';

export const load: PageLoad = async ({ params, url, parent }) => {
	const { queryClient } = await parent();
	return loadExistingSwarmProjectEditorData(queryClient, decodeURIComponent(params.name), url.searchParams.get('templateId'));
};
