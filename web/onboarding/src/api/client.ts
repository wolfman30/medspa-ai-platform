import { fetchAuthSession } from 'aws-amplify/auth';
import { isCognitoConfigured } from '../auth/config';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';
const ONBOARDING_TOKEN = import.meta.env.VITE_ONBOARDING_TOKEN || '';
export type ApiScope = 'admin' | 'portal';

function scopedBasePath(scope: ApiScope): string {
  return scope === 'portal' ? 'portal' : 'admin';
}

async function getAccessToken(): Promise<string | null> {
  if (!isCognitoConfigured()) return null;
  try {
    const session = await fetchAuthSession();
    return session.tokens?.accessToken?.toString() || null;
  } catch {
    return null;
  }
}

function buildHeaders(
  token?: string | null,
  extra?: Record<string, string>
): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  if (ONBOARDING_TOKEN) {
    headers['X-Onboarding-Token'] = ONBOARDING_TOKEN;
  }
  if (extra) {
    Object.assign(headers, extra);
  }
  return headers;
}

async function getHeaders(
  extra?: Record<string, string>
): Promise<Record<string, string>> {
  const token = await getAccessToken();
  return buildHeaders(token, extra);
}

async function readErrorMessage(res: Response): Promise<string> {
  const text = await res.text();
  if (!text) return 'Unknown error';
  try {
    const data = JSON.parse(text) as { error?: string };
    if (data?.error) {
      return data.error;
    }
  } catch {
    // Non-JSON error body; fall through.
  }
  return text;
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

export interface PortalDashboardOverview {
  org_id: string;
  period_start: string;
  period_end: string;
  conversations: number;
  successful_deposits: number;
  conversion_pct: number;
}

export interface DayHours {
  open: string;
  close: string;
}

export interface BusinessHours {
  monday?: DayHours | null;
  tuesday?: DayHours | null;
  wednesday?: DayHours | null;
  thursday?: DayHours | null;
  friday?: DayHours | null;
  saturday?: DayHours | null;
  sunday?: DayHours | null;
}

export interface PrefillService {
  name: string;
  description: string;
  duration_minutes: number;
  price_range: string;
  source_url?: string;
}

export interface PrefillResult {
  clinic_info: {
    name?: string;
    email?: string;
    phone?: string;
    address?: string;
    city?: string;
    state?: string;
    zip_code?: string;
    website_url?: string;
    timezone?: string;
  };
  services: PrefillService[];
  business_hours: BusinessHours;
  sources?: string[];
  warnings?: string[];
}

export interface ClinicConfig {
  org_id: string;
  name: string;
  email?: string;
  phone?: string;
  address?: string;
  city?: string;
  state?: string;
  zip_code?: string;
  website_url?: string;
  timezone: string;
  clinic_info_confirmed?: boolean;
  business_hours_confirmed?: boolean;
  services_confirmed?: boolean;
  contact_info_confirmed?: boolean;
  business_hours?: BusinessHours;
  services?: string[];
  notifications?: NotificationSettings;
}

export async function createClinic(data: {
  name: string;
  email?: string;
  phone?: string;
  address?: string;
  city?: string;
  state?: string;
  zipCode?: string;
  websiteUrl?: string;
  timezone?: string;
}): Promise<{ org_id: string; name: string; created_at: string; message: string }> {
  const token = await getAccessToken();
  const basePath = token ? 'admin' : 'onboarding';
  const res = await fetch(`${API_BASE}/${basePath}/clinics`, {
    method: 'POST',
    headers: buildHeaders(token),
    body: JSON.stringify({
      name: data.name,
      email: data.email,
      phone: data.phone,
      address: data.address,
      city: data.city,
      state: data.state,
      zip_code: data.zipCode,
      website_url: data.websiteUrl,
      timezone: data.timezone,
    }),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getDashboardStats(orgId: string): Promise<DashboardStats> {
  const res = await fetch(`${API_BASE}/admin/orgs/${orgId}/dashboard`, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getPortalOverview(
  orgId: string,
  options?: { start?: string; end?: string; phone?: string }
): Promise<PortalDashboardOverview> {
  const params = new URLSearchParams();
  if (options?.start) params.set('start', options.start);
  if (options?.end) params.set('end', options.end);
  if (options?.phone) params.set('phone', options.phone);
  const queryString = params.toString();
  const url = `${API_BASE}/portal/orgs/${orgId}/dashboard${queryString ? '?' + queryString : ''}`;

  const res = await fetch(url, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
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
  const token = await getAccessToken();
  const basePath = token ? 'admin/clinics' : 'onboarding/clinics';
  const statusPath = token ? 'onboarding-status' : 'status';
  const res = await fetch(`${API_BASE}/${basePath}/${orgId}/${statusPath}`, {
    headers: buildHeaders(token),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function updateClinicConfig(
  orgId: string,
  config: Record<string, unknown>
): Promise<void> {
  // Ensure we always send a valid JSON object
  const body = config && Object.keys(config).length > 0 ? config : {};
  const token = await getAccessToken();
  const basePath = token ? 'admin/clinics' : 'onboarding/clinics';
  const res = await fetch(`${API_BASE}/${basePath}/${orgId}/config`, {
    method: 'PUT',
    headers: buildHeaders(token),
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
}

export async function getSquareConnectUrl(orgId: string): Promise<string> {
  const token = await getAccessToken();
  const basePath = token ? 'admin/clinics' : 'onboarding/clinics';
  return `${API_BASE}/${basePath}/${orgId}/square/connect`;
}

export async function getClinicConfig(orgId: string): Promise<ClinicConfig> {
  const token = await getAccessToken();
  const basePath = token ? 'admin/clinics' : 'onboarding/clinics';
  const res = await fetch(`${API_BASE}/${basePath}/${orgId}/config`, {
    headers: buildHeaders(token),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function prefillFromWebsite(websiteUrl: string): Promise<PrefillResult> {
  const token = await getAccessToken();
  const basePath = token ? 'admin/onboarding' : 'onboarding';
  const res = await fetch(`${API_BASE}/${basePath}/prefill`, {
    method: 'POST',
    headers: buildHeaders(token),
    body: JSON.stringify({ website_url: websiteUrl }),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function seedKnowledge(
  orgId: string,
  documents: Array<{ title: string; content: string }>
): Promise<{ count: number; embedded: boolean }> {
  const res = await fetch(`${API_BASE}/knowledge/${orgId}`, {
    method: 'POST',
    headers: await getHeaders({ 'X-Org-Id': orgId }),
    body: JSON.stringify({ documents }),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
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
  options?: { page?: number; pageSize?: number; phone?: string },
  scope: ApiScope = 'admin'
): Promise<ConversationsListResponse> {
  const params = new URLSearchParams();
  if (options?.page) params.set('page', options.page.toString());
  if (options?.pageSize) params.set('page_size', options.pageSize.toString());
  if (options?.phone) params.set('phone', options.phone);

  const queryString = params.toString();
  const url = `${API_BASE}/${scopedBasePath(scope)}/orgs/${orgId}/conversations${queryString ? '?' + queryString : ''}`;

  const res = await fetch(url, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getConversation(
  orgId: string,
  conversationId: string,
  scope: ApiScope = 'admin'
): Promise<ConversationDetailResponse> {
  const res = await fetch(
    `${API_BASE}/${scopedBasePath(scope)}/orgs/${orgId}/conversations/${encodeURIComponent(conversationId)}`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

// Deposits API

export async function listDeposits(
  orgId: string,
  options?: { page?: number; pageSize?: number; status?: string; phone?: string },
  scope: ApiScope = 'admin'
): Promise<DepositsListResponse> {
  const params = new URLSearchParams();
  if (options?.page) params.set('page', options.page.toString());
  if (options?.pageSize) params.set('page_size', options.pageSize.toString());
  if (options?.status) params.set('status', options.status);
  if (options?.phone) params.set('phone', options.phone);

  const queryString = params.toString();
  const url = `${API_BASE}/${scopedBasePath(scope)}/orgs/${orgId}/deposits${queryString ? '?' + queryString : ''}`;

  const res = await fetch(url, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getDeposit(
  orgId: string,
  depositId: string,
  scope: ApiScope = 'admin'
): Promise<DepositDetailResponse> {
  const res = await fetch(
    `${API_BASE}/${scopedBasePath(scope)}/orgs/${orgId}/deposits/${depositId}`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getDepositStats(
  orgId: string,
  scope: ApiScope = 'admin'
): Promise<DepositStatsResponse> {
  const res = await fetch(
    `${API_BASE}/${scopedBasePath(scope)}/orgs/${orgId}/deposits/stats`,
    {
      headers: await getHeaders(),
    }
  );
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
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
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}
