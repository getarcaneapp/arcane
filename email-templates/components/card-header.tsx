import { Column, Heading, Row, Text } from 'react-email';
import { colors, fonts, radii } from '../theme';

export default function CardHeader({ title, warning }: { title: string; warning?: boolean }) {
	return (
		<Row>
			<Column>
				<Heading as="h1" style={titleStyle}>
					{title}
				</Heading>
			</Column>
			<Column align="right">{warning && <Text style={warningStyle}>Warning</Text>}</Column>
		</Row>
	);
}

const titleStyle = {
	fontSize: '24px',
	fontWeight: 'bold' as const,
	margin: 0,
	color: colors.textPrimary,
	fontFamily: fonts.mono
};

const warningStyle = {
	backgroundColor: colors.warningBg,
	color: colors.warningText,
	padding: '4px 12px',
	borderRadius: radii.badge,
	fontSize: '12px',
	fontWeight: '600' as const,
	display: 'inline-block',
	margin: 0
};
