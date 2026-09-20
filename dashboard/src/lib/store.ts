// Simple in-memory store for Watchdog Agent data

export type ClusterData = {
  snapshot: any;
  recommendations: any[];
  lastUpdated: string;
};

// Use a global variable to persist across hot reloads in development
const globalStore = global as unknown as { watchdogData: Record<string, ClusterData> };

if (!globalStore.watchdogData) {
  globalStore.watchdogData = {};
}

export const getStore = () => globalStore.watchdogData;

export const updateClusterData = (clusterId: string, snapshot: any, recommendations: any[]) => {
  globalStore.watchdogData[clusterId] = {
    snapshot,
    recommendations,
    lastUpdated: new Date().toISOString()
  };
};
