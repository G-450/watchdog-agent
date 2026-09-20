import { NextResponse } from 'next/server';
import { updateClusterData } from '@/lib/store';

export async function POST(request: Request) {
  try {
    const data = await request.json();
    
    // In a real application, cluster ID might come from auth or payload.
    // We'll use a default or infer from namespaces if available.
    let clusterId = 'cluster-default';
    
    if (data.snapshot && data.snapshot.namespaces) {
      // Just a simple way to uniquely identify for now
      clusterId = 'cluster-' + Object.keys(data.snapshot.namespaces).length + '-ns';
    }

    updateClusterData(clusterId, data.snapshot, data.recommendations);
    
    console.log(`[Ingest API] Received data for cluster: ${clusterId}`);
    return NextResponse.json({ success: true, clusterId });
  } catch (error) {
    console.error('[Ingest API] Error parsing incoming data:', error);
    return NextResponse.json({ success: false, error: 'Invalid payload' }, { status: 400 });
  }
}
