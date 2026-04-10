import { Column, Row, Section, Text } from '@react-email/components';
import { BaseTemplate } from '../components/base-template';
import CardHeader from '../components/card-header';
import { sharedPreviewProps, sharedTemplateProps } from '../props';

interface TestEmailProps {
	logoURL: string;
	appURL: string;
	environment: string;
}

export const TestEmail = ({ logoURL, appURL, environment }: TestEmailProps) => (
	<BaseTemplate logoURL={logoURL} appURL={appURL}>
		<CardHeader title="Test Email" />
		<Text style={textStyle}>Your email setup is working correctly!</Text>

		<Section style={infoSectionStyle}>
			<Row style={infoRowStyle}>
				<Column style={labelColumnStyle}>
					<Text style={labelStyle}>Environment:</Text>
				</Column>
				<Column>
					<Text style={valueStyle}>{environment}</Text>
				</Column>
			</Row>
		</Section>
	</BaseTemplate>
);

export default TestEmail;

const textStyle = {
	fontSize: '16px',
	lineHeight: '24px',
	color: '#cbd5e1',
	marginTop: '16px',
	marginBottom: '0'
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

TestEmail.TemplateProps = {
	...sharedTemplateProps
};

TestEmail.PreviewProps = {
	...sharedPreviewProps
};
