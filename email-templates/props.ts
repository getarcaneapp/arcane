export const sharedPreviewProps = {
	logoURL:
		'https://raw.githubusercontent.com/ofkm/arcane/main/backend/resources/images/logo-full.svg',
	appURL: 'http://localhost:3552',
	environment: 'Homelab Production'
};

export const sharedTemplateProps = {
	logoURL: '{{.LogoURL}}',
	appURL: '{{.AppURL}}',
	environment: '{{.Environment}}'
};
