import { Column, Hr, Row, Section, Text } from '@react-email/components';
import { BaseTemplate } from '../components/base-template';
import CardHeader from '../components/card-header';
import { sharedPreviewProps, sharedTemplateProps } from '../props';

interface ContainerUpdateEmailProps {
	logoURL: string;
	appURL: string;
	environment: string;
	containerName: string;
	imageRef: string;
	oldDigest: string;
	newDigest: string;
	updateTime: string;
}

export const ContainerUpdateEmail = ({
	logoURL,
	appURL,
	environment,
	containerName,
	imageRef,
	oldDigest,
	newDigest,
	updateTime
}: ContainerUpdateEmailProps) => {
	const truncateDigest = (digest: string) => {
		if (digest.length > 19) {
			return digest.substring(0, 19) + '...';
		}
		return digest;
	};

	return (
		<BaseTemplate logoURL={logoURL} appURL={appURL}>
			<CardHeader title="Container Successfully Updated" />

			<Section style={{ marginTop: '24px' }}>
				<Text style={mainTextStyle}>
					Your container has been successfully updated with the latest image version.
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
						<Text style={labelStyle}>Container:</Text>
					</Column>
					<Column>
						<Text style={valueStyle}>{containerName}</Text>
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
						<Text style={statusStyle}>✓ Updated Successfully</Text>
					</Column>
				</Row>

				{oldDigest && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Previous Version:</Text>
							</Column>
							<Column>
								<Text style={digestStyle}>{truncateDigest(oldDigest)}</Text>
							</Column>
						</Row>
					</>
				)}

				{newDigest && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Current Version:</Text>
							</Column>
							<Column>
								<Text style={digestStyle}>{truncateDigest(newDigest)}</Text>
							</Column>
						</Row>
					</>
				)}

				{updateTime && (
					<>
						<Hr style={dividerStyle} />
						<Row style={infoRowStyle}>
							<Column style={labelColumnStyle}>
								<Text style={labelStyle}>Updated At:</Text>
							</Column>
							<Column>
								<Text style={valueStyle}>{updateTime}</Text>
							</Column>
						</Row>
					</>
				)}
			</Section>

			<Section style={{ marginTop: '24px' }}>
				<Text style={footerStyle}>
					This is an automated notification from Arcane. Your container has been restarted with the
					new image version.
				</Text>
			</Section>
		</BaseTemplate>
	);
};

export default ContainerUpdateEmail;

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

const statusStyle = {
	fontSize: '14px',
	fontWeight: '600' as const,
	color: '#34d399',
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

ContainerUpdateEmail.TemplateProps = {
	...sharedTemplateProps,
	containerName: '{{.ContainerName}}',
	imageRef: '{{.ImageRef}}',
	oldDigest: '{{.OldDigest}}',
	newDigest: '{{.NewDigest}}',
	updateTime: '{{.UpdateTime}}'
};

ContainerUpdateEmail.PreviewProps = {
	...sharedPreviewProps,
	containerName: 'my-app-container',
	imageRef: 'nginx:latest',
	oldDigest: 'sha256:abc123def456789012345678901234567890',
	newDigest: 'sha256:xyz789ghi012345678901234567890123456',
	updateTime: '2025-10-27 15:30:00 UTC'
};
