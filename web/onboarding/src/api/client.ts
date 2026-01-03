import { fetchAuthSession } from 'aws-amplify/auth';
import { isCognitoConfigured } from '../auth/config';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

// Get headers with optional auth token
async function getHeaders(): Promise<Record<string, string>> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };

  if (isCognitoConfigured()) {
    try {
      const session = await fetchAuthSession();
      const token = session.tokens?.accessToken?.toString();
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }
    } catch {
      // No session available, continue without auth
    }
  }

  return headers;
}

export async function createClinic(data: {
  name: string;
  email?: string;
  phone?: string;
  timezone?: string;
}): Promise<{ org_id: string; name: string; created_at: string; message: string }> {
  const res = await fetch(`${API_BASE}/onboarding/clinics`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to create clinic');
  }
  return res.json();
}

export async function getOnboardingStatus(orgId: string): Promise<{
  org_id: string;
  clinic_name: string;
  overall_progress: number;
  ready_for_launch: boolean;
  steps: Array<{
    id: string;
    name: string;
    description: string;
    completed: boolean;
    required: boolean;
  }>;
  next_action?: string;
  next_action_url?: string;
}> {
  const res = await fetch(`${API_BASE}/onboarding/clinics/${orgId}/status`, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to get onboarding status');
  }
  return res.json();
}

export async function updateClinicConfig(
  orgId: string,
  config: Record<string, unknown>
): Promise<void> {
  // Ensure we always send a valid JSON object
  const body = config && Object.keys(config).length > 0 ? config : {};
  const res = await fetch(`${API_BASE}/onboarding/clinics/${orgId}/config`, {
    method: 'PUT',
    headers: await getHeaders(),
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to update config');
  }
}

export function getSquareConnectUrl(orgId: string): string {
  return `${API_BASE}/onboarding/clinics/${orgId}/square/connect`;
}

export async function seedKnowledge(
  orgId: string,
  documents: Array<{ title: string; content: string }>
): Promise<{ count: number; embedded: boolean }> {
  const res = await fetch(`${API_BASE}/knowledge/${orgId}`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify({ documents }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to seed knowledge');
  }
  return res.json();
}
