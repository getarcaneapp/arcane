export interface NetworkUsageCounts {
  inuse: number;
  unused: number;
  total: number;
}

export type NetworkSummary = {
  id: string;
  name: string;
  driver?: string;
  scope?: string;
};
