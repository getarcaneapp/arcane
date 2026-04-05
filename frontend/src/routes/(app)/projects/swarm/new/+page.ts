import type { PageLoad } from './$types';
import { loadNewSwarmProjectEditorData } from '../editor-data';

export const load: PageLoad = async ({ url, parent }) => {
	const { queryClient } = await parent();
	return loadNewSwarmProjectEditorData(queryClient, url.searchParams.get('templateId'));
};
