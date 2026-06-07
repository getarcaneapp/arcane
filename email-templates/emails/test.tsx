import { Column, Row, Section, Text } from 'react-email';
import { BaseTemplate } from '../components/base-template';
import CardHeader from '../components/card-header';
import { sharedPreviewProps, sharedTemplateProps } from '../props';
import { colors, radii } from '../theme';

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
	color: colors.textBody,
	marginTop: '16px',
	marginBottom: '0'
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
	width: '140px',
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

TestEmail.TemplateProps = {
	...sharedTemplateProps
};

TestEmail.PreviewProps = {
	...sharedPreviewProps
};
