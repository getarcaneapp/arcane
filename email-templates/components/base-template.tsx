import { Body, Container, Head, Html, Img, Link, Section, Text } from '@react-email/components';

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
					<div style={glassCard}>{children}</div>
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
	backgroundColor: '#0f172a',
	fontFamily:
		"-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
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

const glassCard = {
	backgroundColor: 'rgba(30, 41, 59, 0.6)',
	backdropFilter: 'blur(20px)',
	WebkitBackdropFilter: 'blur(20px)',
	border: '1px solid rgba(148, 163, 184, 0.1)',
	padding: '32px',
	borderRadius: '16px',
	boxShadow: '0 8px 32px 0 rgba(0, 0, 0, 0.37)'
};

const footerSection = {
	textAlign: 'center' as const,
	marginTop: '32px',
	paddingTop: '24px'
};

const footerText = {
	margin: '0',
	fontSize: '14px',
	lineHeight: '20px'
};

const footerLink = {
	color: '#a78bfa',
	textDecoration: 'none',
	fontWeight: '500' as const
};
