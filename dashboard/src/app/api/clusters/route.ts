import { NextResponse } from 'next/server';
import { getStore } from '@/lib/store';

export async function GET() {
  const store = await getStore();
  
  // Format the data for the frontend
  const clusters = Object.keys(store).map((key) => {
    return {
      id: key,
      data: store[key]
    };
  });
  
  return NextResponse.json({ clusters });
}
