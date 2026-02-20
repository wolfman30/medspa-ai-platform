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
    // Prefer ID token so portal ownership checks can use the email claim.
    return session.tokens?.idToken?.toString() || session.tokens?.accessToken?.toString() || null;
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
  total_collected_cents: number;
  conversion_pct: number;
}

export interface SquareStatus {
  connected: boolean;
  org_id: string;
  merchant_id?: string;
  location_id?: string;
  phone_number?: string;
  token_expires_at?: string;
  token_expired?: boolean;
  refresh_token_present?: boolean;
  connected_at?: string;
  last_refresh_attempt_at?: string;
  last_refresh_failure_at?: string;
  last_refresh_error?: string;
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
  booking_platform?: string;
  booking_url?: string;
}

export async function createClinic(data: {
  name: string;
  legalName?: string;
  ein?: string;
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
      legal_name: data.legalName,
      ein: data.ein,
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

export async function getSquareStatus(
  orgId: string,
  scope: ApiScope = 'portal'
): Promise<SquareStatus> {
  const path =
    scope === 'portal'
      ? `portal/orgs/${orgId}/square/status`
      : `admin/clinics/${orgId}/square/status`;
  const res = await fetch(`${API_BASE}/${path}`, {
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
  setup_complete: boolean;
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

// ── Stripe Connect ──

export interface StripeStatus {
  connected: boolean;
  account_id?: string;
}

export async function getStripeStatus(
  orgId: string,
  scope: ApiScope = 'portal'
): Promise<StripeStatus> {
  const path =
    scope === 'portal'
      ? `portal/orgs/${orgId}/stripe/status`
      : `admin/clinics/${orgId}/stripe/status`;
  const res = await fetch(`${API_BASE}/${path}`, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getStripeConnectUrl(orgId: string): Promise<string> {
  const token = await getAccessToken();
  const basePath = token ? 'admin/clinics' : 'onboarding/clinics';
  return `${API_BASE}/${basePath}/${orgId}/stripe/connect`;
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

export async function getPortalKnowledge(orgId: string): Promise<{ documents: unknown[] }> {
  const token = await getAccessToken();
  const res = await fetch(`${API_BASE}/portal/orgs/${orgId}/knowledge`, {
    headers: buildHeaders(token),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function updatePortalKnowledge(
  orgId: string,
  documents: unknown[]
): Promise<{ documents: number; status: string }> {
  const token = await getAccessToken();
  const res = await fetch(`${API_BASE}/portal/orgs/${orgId}/knowledge`, {
    method: 'PUT',
    headers: buildHeaders(token),
    body: JSON.stringify({ documents }),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function getAdminKnowledge(orgId: string): Promise<{ documents: unknown[] }> {
  const token = await getAccessToken();
  const res = await fetch(`${API_BASE}/admin/clinics/${orgId}/knowledge`, {
    headers: buildHeaders(token),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function updateAdminKnowledge(
  orgId: string,
  documents: unknown[]
): Promise<{ documents: number; status: string }> {
  const token = await getAccessToken();
  const res = await fetch(`${API_BASE}/admin/clinics/${orgId}/knowledge`, {
    method: 'PUT',
    headers: buildHeaders(token),
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

// LOA / Hosted Number API

export interface CheckEligibilityResponse {
  eligible: boolean;
  phone_type?: string;
  reason?: string;
}

export async function checkHostedEligibility(phoneNumber: string): Promise<CheckEligibilityResponse> {
  const res = await fetch(`${API_BASE}/admin/messaging/hosted/eligibility`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify({ phone_number: phoneNumber }),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export interface StartHostedOrderRequest {
  clinic_id: string;
  phone_number: string;
  billing_number?: string;
  contact_name: string;
  contact_email: string;
  contact_phone?: string;
}

export interface HostedOrderResponse {
  id: string;
  status: string;
  phone_number: string;
}

export async function startHostedOrder(data: StartHostedOrderRequest): Promise<HostedOrderResponse> {
  const res = await fetch(`${API_BASE}/admin/messaging/hosted/orders`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function uploadLOADocument(orderId: string, file: File): Promise<void> {
  const token = await getAccessToken();
  const formData = new FormData();
  formData.append('file', file);
  formData.append('document_type', 'letter_of_authorization');

  const headers: Record<string, string> = {};
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (ONBOARDING_TOKEN) headers['X-Onboarding-Token'] = ONBOARDING_TOKEN;

  const res = await fetch(`${API_BASE}/admin/messaging/hosted/orders/${orderId}/documents`, {
    method: 'POST',
    headers,
    body: formData,
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
}

export async function uploadPhoneBill(orderId: string, file: File): Promise<void> {
  const token = await getAccessToken();
  const formData = new FormData();
  formData.append('file', file);
  formData.append('document_type', 'phone_bill');

  const headers: Record<string, string> = {};
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (ONBOARDING_TOKEN) headers['X-Onboarding-Token'] = ONBOARDING_TOKEN;

  const res = await fetch(`${API_BASE}/admin/messaging/hosted/orders/${orderId}/documents`, {
    method: 'POST',
    headers,
    body: formData,
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
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

// AI Persona Settings API

export interface AIPersona {
  provider_name?: string;
  is_solo_operator?: boolean;
  tone?: 'clinical' | 'warm' | 'professional';
  custom_greeting?: string;
  after_hours_greeting?: string;
  busy_message?: string;
  special_services?: string[];
}

export async function getAIPersona(orgId: string): Promise<AIPersona> {
  const res = await fetch(`${API_BASE}/admin/clinics/${orgId}/config`, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  const config = await res.json();
  return config.ai_persona || {};
}

export async function updateAIPersona(
  orgId: string,
  persona: AIPersona
): Promise<AIPersona> {
  const res = await fetch(`${API_BASE}/admin/clinics/${orgId}/config`, {
    method: 'PUT',
    headers: await getHeaders(),
    body: JSON.stringify({ ai_persona: persona }),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  const config = await res.json();
  return config.ai_persona || {};
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

// Admin: List all organizations
export interface OrgListItem {
  id: string;
  name: string;
  owner_email?: string;
  created_at: string;
}

export interface ListOrgsResponse {
  organizations: OrgListItem[];
  total: number;
}

export async function listOrgs(): Promise<ListOrgsResponse> {
  const res = await fetch(`${API_BASE}/admin/orgs`, {
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

// Structured Knowledge API

import type { StructuredKnowledge } from '../types/knowledge';

export async function getStructuredKnowledge(
  orgId: string,
  scope: ApiScope
): Promise<StructuredKnowledge | null> {
  const path =
    scope === 'portal'
      ? `portal/orgs/${orgId}/knowledge/structured`
      : `admin/clinics/${orgId}/knowledge/structured`;
  const res = await fetch(`${API_BASE}/${path}`, {
    headers: await getHeaders(),
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

export async function updateStructuredKnowledge(
  orgId: string,
  scope: ApiScope,
  data: StructuredKnowledge
): Promise<void> {
  const path =
    scope === 'portal'
      ? `portal/orgs/${orgId}/knowledge/structured`
      : `admin/clinics/${orgId}/knowledge/structured`;
  const res = await fetch(`${API_BASE}/${path}`, {
    method: 'PUT',
    headers: await getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
}

export async function syncMoxieKnowledge(
  orgId: string,
  scope: ApiScope
): Promise<StructuredKnowledge> {
  const path =
    scope === 'portal'
      ? `portal/orgs/${orgId}/knowledge/sync-moxie`
      : `admin/clinics/${orgId}/knowledge/sync-moxie`;
  const res = await fetch(`${API_BASE}/${path}`, {
    method: 'POST',
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

// Patient Data Management API

/**
 * Clear all patient data for an organization.
 * This removes conversations, leads, deposits, payments, bookings, and messages
 * but preserves clinic configuration and knowledge.
 */
export async function clearAllPatientData(orgId: string): Promise<{ message: string }> {
  const res = await fetch(`${API_BASE}/admin/clinics/${orgId}/data`, {
    method: 'DELETE',
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

/**
 * Clear patient data for a specific phone number.
 * This removes conversations, leads, deposits, payments, and messages
 * associated with the given phone number.
 */
export async function clearPatientDataByPhone(
  orgId: string,
  phone: string
): Promise<{ message: string }> {
  const encodedPhone = encodeURIComponent(phone);
  const res = await fetch(`${API_BASE}/admin/clinics/${orgId}/phones/${encodedPhone}`, {
    method: 'DELETE',
    headers: await getHeaders(),
  });
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return res.json();
}

// ── Prospects ───────────────────────────────────────────────────────

export async function listProspects(): Promise<{ lastUpdated: string; prospects: unknown[] }> {
  const res = await fetch(`${API_BASE}/admin/prospects`, {
    headers: await getHeaders(),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function upsertProspect(id: string, data: Record<string, unknown>): Promise<void> {
  const res = await fetch(`${API_BASE}/admin/prospects/${id}`, {
    method: 'PUT',
    headers: await getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
}

export interface ProspectOutreach {
  draft: string;
  research: string;
  draftExists: boolean;
  researchExists: boolean;
}

export async function getProspectOutreach(prospectId: string): Promise<ProspectOutreach> {
  const res = await fetch(`${API_BASE}/admin/prospects/${prospectId}/outreach`, {
    headers: await getHeaders(),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// ── Morning Briefs ──────────────────────────────────────────────────

export interface MorningBrief {
  id: string;
  title: string;
  date: string;
  content: string;
}

export async function listBriefs(): Promise<{ briefs: MorningBrief[] }> {
  const res = await fetch(`${API_BASE}/admin/briefs`, {
    headers: await getHeaders(),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function getBrief(date: string): Promise<MorningBrief> {
  const res = await fetch(`${API_BASE}/admin/briefs/${date}`, {
    headers: await getHeaders(),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// ── Rule of 100 ─────────────────────────────────────────────────────

export interface Rule100ProspectCount {
  id: string;
  clinic: string;
  count: number;
}

export interface Rule100DayHistory {
  date: string;
  touches: number;
}

export interface Rule100Response {
  date: string;
  touches: number;
  goal: number;
  streak: number;
  byType: Record<string, number>;
  byProspect: Rule100ProspectCount[];
  history: Rule100DayHistory[];
}

export async function getRule100Today(): Promise<Rule100Response> {
  const res = await fetch(`${API_BASE}/admin/rule100/today`, {
    headers: await getHeaders(),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function logTouch(prospectId: string, type: string, note?: string): Promise<unknown> {
  const res = await fetch(`${API_BASE}/admin/prospects/${prospectId}/events`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify({ type, note: note || 'Rule of 100 touch' }),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function addProspectEvent(prospectId: string, type: string, note: string): Promise<unknown> {
  const res = await fetch(`${API_BASE}/admin/prospects/${prospectId}/events`, {
    method: 'POST',
    headers: await getHeaders(),
    body: JSON.stringify({ type, note }),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
