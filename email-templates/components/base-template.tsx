import { Body, Container, Head, Html, Img, Link, Section, Text } from 'react-email';
import { colors, fonts, radii } from '../theme';

interface BaseTemplateProps {
	logoURL?: string;
	appURL?: string;
	children: React.ReactNode;
}

export const BaseTemplate = ({ logoURL, appURL, children }: BaseTemplateProps) => {
	return (
		<Html>
			<Head />
			<Body style={mainStyle}>
				<Container style={{ width: '600px', margin: '0 auto' }}>
					<Section style={logoSection}>
						<Img src={logoURL} width="180" height="auto" alt="Arcane" style={logoStyle} />
					</Section>
					<div style={cardStyle}>{children}</div>
					{appURL && (
						<Section style={footerSection}>
							<Text style={footerText}>
								<Link href={appURL} style={footerLink}>
									Open Arcane Dashboard →
								</Link>
							</Text>
						</Section>
					)}
				</Container>
			</Body>
		</Html>
	);
};

const mainStyle = {
	padding: '40px 20px',
	backgroundColor: colors.bg,
	fontFamily: fonts.sans
};

const logoSection = {
	textAlign: 'center' as const,
	marginBottom: '32px'
};

const logoStyle = {
	width: '180px',
	height: 'auto',
	display: 'inline-block'
};

const cardStyle = {
	backgroundColor: colors.card,
	border: `1px solid ${colors.cardBorder}`,
	padding: '32px',
	borderRadius: radii.card,
	boxShadow: 'none'
};

const footerSection = {
	textAlign: 'center' as const,
	marginTop: '32px',
	paddingTop: '24px'
};

const footerText = {
	margin: '0',
	fontSize: '14px',
	lineHeight: '20px',
	color: colors.textMuted
};

const footerLink = {
	color: colors.accent,
	textDecoration: 'none',
	fontWeight: '500' as const
};
