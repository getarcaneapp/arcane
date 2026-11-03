export interface ImageAttestationRequestOptions {
	platform?: string;
	predicateType?: string;
	statement?: boolean;
}

export type ImagePullResult = {
	success: boolean;
	imageName: string;
	error?: string;
	/** The pull was abandoned because the user signed out. */
	cancelled?: boolean;
};
