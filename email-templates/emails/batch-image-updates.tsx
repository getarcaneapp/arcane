import { Column, Hr, Row, Section, Text } from 'react-email';
import { BaseTemplate } from '../components/base-template';
import CardHeader from '../components/card-header';
import { sharedPreviewProps, sharedTemplateProps } from '../props';
import { colors, fonts, radii } from '../theme';

interface BatchImageUpdatesEmailProps {
	logoURL: string;
	appURL: string;
	environment: string;
	updateCount: number;
	checkTime: string;
	imageList?: string[];
}

export const BatchImageUpdatesEmail = ({
	logoURL,
	appURL,
	environment,
	updateCount,
	checkTime,
	imageList = []
}: BatchImageUpdatesEmailProps) => {
	// Handle both array (preview) and string (template placeholder)
	const images = Array.isArray(imageList) ? imageList : [];

	// For template generation, always include image list section with placeholder text
	// This will be replaced with Go template range syntax
	const showImageList = images.length > 0 || typeof imageList === 'string';

	return (
		<BaseTemplate logoURL={logoURL} appURL={appURL}>
			<CardHeader title="Image Updates Available" />

			<Section style={{ marginTop: '24px' }}>
				<Text style={mainTextStyle}>
					{updateCount === 1
						? `1 container image has an update available.`
						: `${updateCount} container images have updates available.`}
				</Text>
			</Section>

			<Section style={infoSectionStyle}>
				<Row style={infoRowStyle}>
					<Column style={labelColumnStyle}>
						<Text style={labelStyle}>Environment:</Text>
					</Column>
					<Column>
						<Text style={valueStyle}>{environment}</Text>
					</Column>
				</Row>

				<Hr style={dividerStyle} />

				<Row style={infoRowStyle}>
					<Column style={labelColumnStyle}>
						<Text style={labelStyle}>Updates Available:</Text>
					</Column>
					<Column>
						<Text style={countStyle}>{updateCount}</Text>
					</Column>
				</Row>

				<Hr style={dividerStyle} />

				<Row style={infoRowStyle}>
					<Column style={labelColumnStyle}>
						<Text style={labelStyle}>Checked At:</Text>
					</Column>
					<Column>
						<Text style={valueStyle}>{checkTime}</Text>
					</Column>
				</Row>
			</Section>

			{showImageList && (
				<Section style={imageListSectionStyle}>
					<Text style={imageListHeaderStyle}>Images with Updates:</Text>
					{images.length > 0 ? (
						images.map((image, index) => (
							<Text key={index} style={imageItemStyle}>
								• {image}
							</Text>
						))
					) : (
						<Text style={imageItemStyle}>IMAGELIST_PLACEHOLDER</Text>
					)}
				</Section>
			)}

			<Section style={{ marginTop: '24px' }}>
				<Text style={footerStyle}>
					Log in to Arcane to view details and update your containers.
				</Text>
			</Section>
		</BaseTemplate>
	);
};

export default BatchImageUpdatesEmail;

const mainTextStyle = {
	fontSize: '16px',
	lineHeight: '24px',
	color: colors.textBody,
	margin: '0 0 16px 0'
};

const infoSectionStyle = {
	marginTop: '20px',
	backgroundColor: colors.panel,
	border: `1px solid ${colors.panelBorder}`,
	padding: '20px',
	borderRadius: radii.panel
};

const infoRowStyle = {
	marginBottom: '0'
};

const labelColumnStyle = {
	width: '160px',
	verticalAlign: 'top' as const,
	paddingRight: '12px'
};

const labelStyle = {
	fontSize: '14px',
	fontWeight: '600' as const,
	color: colors.textMuted,
	margin: '8px 0'
};

const valueStyle = {
	fontSize: '14px',
	color: colors.textValue,
	margin: '8px 0',
	wordBreak: 'break-word' as const
};

const countStyle = {
	fontSize: '24px',
	fontWeight: '700' as const,
	color: colors.success,
	margin: '8px 0'
};

const dividerStyle = {
	borderColor: colors.divider,
	margin: '4px 0'
};

const footerStyle = {
	fontSize: '13px',
	lineHeight: '20px',
	color: colors.textMuted,
	margin: '0'
};

const imageListSectionStyle = {
	marginTop: '24px',
	backgroundColor: colors.panel,
	border: `1px solid ${colors.panelBorder}`,
	padding: '16px',
	borderRadius: radii.panel
};

const imageListHeaderStyle = {
	fontSize: '14px',
	fontWeight: '600' as const,
	color: colors.textMuted,
	margin: '0 0 12px 0'
};

const imageItemStyle = {
	fontSize: '13px',
	lineHeight: '20px',
	color: colors.textBody,
	margin: '4px 0',
	fontFamily: fonts.mono
};

BatchImageUpdatesEmail.TemplateProps = {
	...sharedTemplateProps,
	updateCount: '{{.UpdateCount}}',
	checkTime: '{{.CheckTime}}',
	imageList: 'TEMPLATE_PLACEHOLDER' // This triggers the image list section to render
};

BatchImageUpdatesEmail.PreviewProps = {
	...sharedPreviewProps,
	updateCount: 7,
	checkTime: '2025-10-27 15:30:00 UTC',
	imageList: [
		'ghcr.io/linuxserver/plex:latest',
		'postgres:16-alpine',
		'redis:7.2-alpine',
		'nginx:latest',
		'traefik:v3.0',
		'portainer/portainer-ce:latest',
		'grafana/grafana:latest'
	]
};
