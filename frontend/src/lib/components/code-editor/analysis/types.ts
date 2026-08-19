export type DiagnosticSeverity = 'error' | 'warning' | 'info' | 'hint';

export type DiagnosticEdit = {
	from: number;
	to: number;
	insert: string;
};

export type DiagnosticAction = {
	name: string;
	edits: DiagnosticEdit[];
};

export type Diagnostic = {
	from: number;
	to: number;
	severity: DiagnosticSeverity;
	message: string;
	actions?: DiagnosticAction[];
};

export type CompletionItem = {
	label: string;
	type?: string;
	detail?: string;
	info?: string;
	apply: string;
};

export type SnippetItem = {
	label: string;
	type?: string;
	detail?: string;
	insert: string;
};

export type CodeLanguage =
	| 'yaml'
	| 'env'
	| 'json'
	| 'toml'
	| 'dockerfile'
	| 'shell'
	| 'javascript'
	| 'typescript'
	| 'markdown'
	| 'plaintext';
export type CodeValidationMode = 'compose' | 'env' | 'none';

export type SchemaStatus = 'ready' | 'cached' | 'unavailable';

export type EditorContext = {
	envContent?: string;
	composeContents?: string[];
	globalVariables?: Record<string, string>;
};

export type DiagnosticSummary = {
	errors: number;
	warnings: number;
	infos: number;
	hints: number;
	schemaStatus: SchemaStatus;
	schemaMessage?: string;
	cursorLine: number;
	cursorCol: number;
	validationReady: boolean;
};

export type OutlineItem = {
	id: string;
	label: string;
	path: Array<string | number>;
	from: number;
	to: number;
	level: number;
};

export type SchemaDoc = {
	title?: string;
	description?: string;
	defaultValue?: string;
	examples?: string[];
};

export type AnalysisResult = {
	diagnostics: Diagnostic[];
	outlineItems: OutlineItem[];
	summaryPatch: Partial<DiagnosticSummary>;
};
