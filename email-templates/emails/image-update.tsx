import { Column, Hr, Row, Section, Text } from '@react-email/components';
import { BaseTemplate } from '../components/base-template';
import CardHeader from '../components/card-header';
import { sharedPreviewProps, sharedTemplateProps } from '../props';

interface ImageUpdateEmailProps {
	logoURL: string;
	appURL: string;
	environment: string;
	imageRef: string;
	hasUpdate: boolean;
	updateType: string;
	currentDigest: string;
	latestDigest: string;
	checkTime: string;
}

export const ImageUpdateEmail = ({
	logoURL,
	appURL,
	environment,
	imageRef,
	hasUpdate,
	updateType,
	currentDigest,
	latestDigest,
	checkTime
}: ImageUpdateEmailProps) => {
	const truncateDigest = (digest: string) => {
		if (digest.length > 19) {
			return digest.substring(0, 19) + '...';
		}
		return digest;
	};

	return (
		<BaseTemplate logoURL={logoURL} appURL={appURL}>
			<CardHeader title="Container Image Update" />

			<Section style={{ marginTop: '24px' }}>
				<Text style={mainTextStyle}>
					{hasUpdate
						? `A new update has been detected for your container image.`
						: `Your container image has been checked for updates.`}
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
						<Text style={labelStyle}>Image:</Text>
					</Column>
					<Column>
						<Text style={valueStyle}>{imageRef}</Text>
					</Column>
				</Row>

				<Hr style={dividerStyle} />

				<Row style={infoRowStyle}>
					<Column style={labelColumnStyle}>
						<Text style={labelStyle}>Status:</Text>
					</Column>
					<Column>
						<Text style={hasUpdate ? statusUpdateStyle : statusNoUpdateStyle}>
							{hasUpdate ? '✓ Update Available' : 'Up to Date'}
						</Text>
					</Column>
				</Row>

				{updateType && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Update Type:</Text>
							</Column>
							<Column>
								<Text style={valueStyle}>{updateType}</Text>
							</Column>
						</Row>
					</>
				)}

				{currentDigest && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Current Digest:</Text>
							</Column>
							<Column>
								<Text style={digestStyle}>{truncateDigest(currentDigest)}</Text>
							</Column>
						</Row>
					</>
				)}

				{latestDigest && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Latest Digest:</Text>
							</Column>
							<Column>
								<Text style={digestStyle}>{truncateDigest(latestDigest)}</Text>
							</Column>
						</Row>
					</>
				)}

				{checkTime && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Checked At:</Text>
							</Column>
							<Column>
								<Text style={valueStyle}>{checkTime}</Text>
							</Column>
						</Row>
					</>
				)}
			</Section>

			<Section style={{ marginTop: '24px' }}>
				<Text style={footerStyle}>
					This is an automated notification from Arcane.
					{hasUpdate && ' Please review and update your container when ready.'}
				</Text>
			</Section>
		</BaseTemplate>
	);
};

export default ImageUpdateEmail;

const mainTextStyle = {
	fontSize: '16px',
	lineHeight: '24px',
	color: '#cbd5e1',
	margin: '0 0 16px 0'
};

const infoSectionStyle = {
	marginTop: '20px',
	backgroundColor: 'rgba(15, 23, 42, 0.5)',
	border: '1px solid rgba(148, 163, 184, 0.1)',
	padding: '20px',
	borderRadius: '12px'
};

const infoRowStyle = {
	marginBottom: '0'
};

const labelColumnStyle = {
	width: '140px',
	verticalAlign: 'top' as const,
	paddingRight: '12px'
};

const labelStyle = {
	fontSize: '14px',
	fontWeight: '600' as const,
	color: '#94a3b8',
	margin: '8px 0'
};

const valueStyle = {
	fontSize: '14px',
	color: '#e2e8f0',
	margin: '8px 0',
	wordBreak: 'break-word' as const
};

const digestStyle = {
	fontSize: '13px',
	color: '#e2e8f0',
	fontFamily: "'Courier New', Courier, monospace",
	margin: '8px 0'
};

const statusUpdateStyle = {
	fontSize: '14px',
	fontWeight: '600' as const,
	color: '#34d399',
	margin: '8px 0'
};

const statusNoUpdateStyle = {
	fontSize: '14px',
	fontWeight: '600' as const,
	color: '#94a3b8',
	margin: '8px 0'
};

const dividerStyle = {
	borderColor: 'rgba(148, 163, 184, 0.2)',
	margin: '4px 0'
};

const footerStyle = {
	fontSize: '13px',
	lineHeight: '20px',
	color: '#94a3b8',
	margin: '0'
};

ImageUpdateEmail.TemplateProps = {
	...sharedTemplateProps,
	imageRef: '{{.ImageRef}}',
	hasUpdate: '{{.HasUpdate}}',
	updateType: '{{.UpdateType}}',
	currentDigest: '{{.CurrentDigest}}',
	latestDigest: '{{.LatestDigest}}',
	checkTime: '{{.CheckTime}}'
};

ImageUpdateEmail.PreviewProps = {
	...sharedPreviewProps,
	imageRef: 'nginx:latest',
	hasUpdate: true,
	updateType: 'digest',
	currentDigest: 'sha256:abc123def456789012345678901234567890',
	latestDigest: 'sha256:xyz789ghi012345678901234567890123456',
	checkTime: '2025-10-18 15:30:00 UTC'
};
