import { Button as EmailButton } from 'react-email';
import { colors, fonts, radii } from '../theme';

interface ButtonProps {
	href: string;
	children: React.ReactNode;
	style?: React.CSSProperties;
}

export const Button = ({ href, children, style = {} }: ButtonProps) => {
	const buttonStyle = {
		backgroundColor: colors.accent,
		color: colors.buttonText,
		padding: '12px 24px',
		borderRadius: radii.button,
		fontSize: '15px',
		fontWeight: '500',
		fontFamily: fonts.sans,
		cursor: 'pointer',
		marginTop: '10px',
		...style
	};

	return (
		<div style={buttonContainer}>
			<EmailButton style={buttonStyle} href={href}>
				{children}
			</EmailButton>
		</div>
	);
};

const buttonContainer = {
	textAlign: 'center' as const
};
