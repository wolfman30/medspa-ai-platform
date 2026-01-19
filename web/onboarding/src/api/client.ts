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

export interface DashboardStats {
  org_id: string;
  org_name: string;
  period: string;
  leads: {
    total: number;
    new_this_week: number;
    conversion_rate: number;
    top_sources?: Array<{
      source: string;
      count: number;
    }>;
  };
  conversations: {
    unique_conversations: number;
    total_jobs: number;
    today: number;
    this_week: number;
  };
  payments: {
    total_collected_cents: number;
    this_week_cents: number;
    pending_deposits: number;
    refunded_cents: number;
    dispute_count: number;
  };
  bookings: {
    total: number;
    upcoming: number;
    this_week: number;
    cancelled_count: number;
  };
  compliance: {
    audit_events_today: number;
    supervisor_interventions: number;
    phi_detections: number;
    disclaimers_sent: number;
  };
  onboarding: {
    brand_status: string;
    campaign_status: string;
    numbers_active: number;
    fully_compliant: boolean;
  };
  pending_actions: Array<{
    type: string;
    priority: string;
    description: string;
    count: number;
    link?: string;
  }> | null;
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

export async function getDashboardStats(orgId: string): Promise<DashboardStats> {
  const res = await fetch(`${API_BASE}/admin/orgs/${orgId}/dashboard`, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to get dashboard stats');
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

export async function activatePhoneNumber(
  orgId: string,
  phoneNumber: string
): Promise<{ clinic_id: string; phone_number: string; status: string }> {
  const res = await fetch(`${API_BASE}/admin/hosted/activate`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify({ clinic_id: orgId, phone_number: phoneNumber }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to activate phone number');
  }
  return res.json();
}

// 10DLC Brand and Campaign Registration

export interface CreateBrandRequest {
  clinic_id: string;
  legal_name: string;
  ein: string;
  website: string;
  address_line: string;
  city: string;
  state: string;
  postal_code: string;
  country: string;
  contact_name: string;
  contact_email: string;
  contact_phone: string;
  vertical: string;
}

export interface BrandResponse {
  brand_id: string;
  status: string;
  legal_name?: string;
}

export async function createBrand(data: CreateBrandRequest): Promise<BrandResponse> {
  const res = await fetch(`${API_BASE}/admin/10dlc/brands`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to create brand');
  }
  return res.json();
}

export interface CreateCampaignRequest {
  brand_internal_id: string;
  use_case: string;
  sample_messages: string[];
  help_message: string;
  stop_message: string;
}

export interface CampaignResponse {
  campaign_id: string;
  status: string;
  use_case: string;
}

export async function createCampaign(data: CreateCampaignRequest): Promise<CampaignResponse> {
  const res = await fetch(`${API_BASE}/admin/10dlc/campaigns`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to create campaign');
  }
  return res.json();
}

// Conversation Viewer API

import type {
  ConversationsListResponse,
  ConversationDetailResponse,
} from '../types/conversation';

import type {
  DepositsListResponse,
  DepositDetailResponse,
  DepositStatsResponse,
} from '../types/deposit';

export async function listConversations(
  orgId: string,
  options?: { page?: number; pageSize?: number; phone?: string }
): Promise<ConversationsListResponse> {
  const params = new URLSearchParams();
  if (options?.page) params.set('page', options.page.toString());
  if (options?.pageSize) params.set('page_size', options.pageSize.toString());
  if (options?.phone) params.set('phone', options.phone);

  const queryString = params.toString();
  const url = `${API_BASE}/admin/orgs/${orgId}/conversations${queryString ? '?' + queryString : ''}`;

  const res = await fetch(url, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to list conversations');
  }
  return res.json();
}

export async function getConversation(
  orgId: string,
  conversationId: string
): Promise<ConversationDetailResponse> {
  const res = await fetch(
    `${API_BASE}/admin/orgs/${orgId}/conversations/${encodeURIComponent(conversationId)}`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to get conversation');
  }
  return res.json();
}

// Deposits API

export async function listDeposits(
  orgId: string,
  options?: { page?: number; pageSize?: number; status?: string }
): Promise<DepositsListResponse> {
  const params = new URLSearchParams();
  if (options?.page) params.set('page', options.page.toString());
  if (options?.pageSize) params.set('page_size', options.pageSize.toString());
  if (options?.status) params.set('status', options.status);

  const queryString = params.toString();
  const url = `${API_BASE}/admin/orgs/${orgId}/deposits${queryString ? '?' + queryString : ''}`;

  const res = await fetch(url, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to list deposits');
  }
  return res.json();
}

export async function getDeposit(
  orgId: string,
  depositId: string
): Promise<DepositDetailResponse> {
  const res = await fetch(
    `${API_BASE}/admin/orgs/${orgId}/deposits/${depositId}`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to get deposit');
  }
  return res.json();
}

export async function getDepositStats(
  orgId: string
): Promise<DepositStatsResponse> {
  const res = await fetch(
    `${API_BASE}/admin/orgs/${orgId}/deposits/stats`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to get deposit stats');
  }
  return res.json();
}

// Notification Settings API

export interface NotificationSettings {
  email_enabled: boolean;
  email_recipients: string[];
  sms_enabled: boolean;
  sms_recipients: string[];
  notify_on_payment: boolean;
  notify_on_new_lead: boolean;
}

export async function getNotificationSettings(
  orgId: string
): Promise<NotificationSettings> {
  const res = await fetch(
    `${API_BASE}/admin/orgs/${orgId}/notifications`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to get notification settings');
  }
  return res.json();
}

export async function updateNotificationSettings(
  orgId: string,
  settings: Partial<NotificationSettings>
): Promise<NotificationSettings> {
  const res = await fetch(
    `${API_BASE}/admin/orgs/${orgId}/notifications`,
    {
      method: 'PUT',
      headers: await getHeaders(),
      body: JSON.stringify(settings),
    }
  );
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to update notification settings');
  }
  return res.json();
}

// Client self-service registration types
export interface RegisterClinicRequest {
  clinic_name: string;
  owner_email: string;
  owner_phone?: string;
  timezone?: string;
}

export interface RegisterClinicResponse {
  org_id: string;
  clinic_name: string;
  owner_email: string;
  created_at: string;
  message: string;
}

export interface LookupOrgResponse {
  org_id: string;
  clinic_name: string;
  owner_email: string;
}

// Register a new clinic (called after Cognito signup)
export async function registerClinic(request: RegisterClinicRequest): Promise<RegisterClinicResponse> {
  const res = await fetch(`${API_BASE}/api/client/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Failed to register clinic');
  }
  return res.json();
}

// Lookup org by owner email (for returning users)
export async function lookupOrgByEmail(email: string): Promise<LookupOrgResponse> {
  const res = await fetch(`${API_BASE}/api/client/org?email=${encodeURIComponent(email)}`, {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(err.error || 'Organization not found');
  }
  return res.json();
}
