import { NextResponse } from 'next/server';
import { updateClusterData } from '@/lib/store';

export async function POST(request: Request) {
  try {
    const data = await request.json();
    
    let clusterId = 'cluster-default';
    
    if (data.snapshot && data.snapshot.cluster_id) {
      clusterId = data.snapshot.cluster_id;
    }

    await updateClusterData(clusterId, data.snapshot, data.recommendations);
    
    console.log(`[Ingest API] Received data for cluster: ${clusterId}`);
    return NextResponse.json({ success: true, clusterId });
  } catch (error) {
    console.error('[Ingest API] Error parsing incoming data:', error);
    return NextResponse.json({ success: false, error: 'Invalid payload' }, { status: 400 });
  }
}
