export type ApiKey = {
  id: string;
  name: string;
  description?: string;
  keyPrefix: string;
  userId: string;
  expiresAt?: string;
  lastUsedAt?: string;
  createdAt: string;
  updatedAt?: string;
};
