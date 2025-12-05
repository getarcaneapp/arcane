export interface ImageUsageCounts {
  imagesInuse: number;
  imagesUnused: number;
  totalImages: number;
  totalImageSize: number;
}

export interface ImageUpdateInfo {
  hasUpdate: boolean;
  updateType: string;
  currentVersion?: string;
  latestVersion?: string;
  currentDigest?: string;
  latestDigest?: string;
  checkTime: string;
  responseTimeMs: number;
  error?: string;
  authMethod?: 'none' | 'anonymous' | 'credential' | 'unknown';
  authUsername?: string;
  authRegistry?: string;
  usedCredential?: boolean;
}

export interface ImageUpdateSummary {
  totalImages: number;
  imagesWithUpdates: number;
  digestUpdates: number;
  errorsCount: number;
}
