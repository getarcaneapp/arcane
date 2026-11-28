import { createContext, type Snippet } from 'svelte';

import type { DiagnosticSummary } from './analysis/types';

export type EditorStatus = DiagnosticSummary & {
	diagnostics: number;
	validating: boolean;
};

export type EditorStatusHost = {
	current: (() => EditorStatus) | null;
	actions: Snippet | null;
};

export const [getEditorStatusHost, setEditorStatusHost, hasEditorStatusHost] = createContext<EditorStatusHost>();
