import { Column, Heading, Row, Text } from '@react-email/components';

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
	color: '#f1f5f9'
};

const warningStyle = {
	backgroundColor: '#fbbf24',
	color: '#78350f',
	padding: '4px 12px',
	borderRadius: '12px',
	fontSize: '12px',
	fontWeight: '600' as const,
	display: 'inline-block',
	margin: 0
};
