import fs from 'fs/promises';
import path from 'path';
import os from 'os';

export type ClusterData = {
  snapshot: any;
  recommendations: any[];
  lastUpdated: string;
};

// Use the OS temp directory for serverless environments
const DATA_FILE = path.join(os.tmpdir(), 'watchdog-data.json');

export const getStore = async (): Promise<Record<string, ClusterData>> => {
  try {
    const data = await fs.readFile(DATA_FILE, 'utf-8');
    return JSON.parse(data);
  } catch (error: any) {
    // If file doesn't exist or is invalid, return empty object
    if (error.code === 'ENOENT') {
      return {};
    }
    console.error('[Store] Error reading data file:', error);
    return {};
  }
};

export const updateClusterData = async (clusterId: string, snapshot: any, recommendations: any[]) => {
  try {
    const store = await getStore();
    store[clusterId] = {
      snapshot,
      recommendations,
      lastUpdated: new Date().toISOString()
    };
    await fs.writeFile(DATA_FILE, JSON.stringify(store, null, 2));
  } catch (error) {
    console.error('[Store] Error writing data file:', error);
  }
};
