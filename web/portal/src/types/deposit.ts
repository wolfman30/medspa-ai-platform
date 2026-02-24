// Types for deposit viewer

export interface DepositListItem {
  id: string;
  org_id: string;
  lead_id?: string;
  lead_phone: string;
  lead_name?: string;
  lead_email?: string;
  service_interest?: string;
  patient_type?: string;
  amount_cents: number;
  status: string;
  provider: string;
  provider_ref?: string;
  scheduled_for?: string;
  created_at: string;
}

export interface DepositsListResponse {
  deposits: DepositListItem[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface DepositDetailResponse {
  id: string;
  org_id: string;
  lead_id?: string;
  lead_phone: string;
  lead_name?: string;
  lead_email?: string;
  service_interest?: string;
  patient_type?: string;
  preferred_days?: string;
  preferred_times?: string;
  scheduling_notes?: string;
  amount_cents: number;
  status: string;
  provider: string;
  provider_ref?: string;
  booking_intent_id?: string;
  scheduled_for?: string;
  created_at: string;
  conversation_id?: string;
}

export interface DepositStatsResponse {
  total_deposits: number;
  total_amount_cents: number;
  by_status: Record<string, number>;
  today_count: number;
  today_amount_cents: number;
  week_count: number;
  week_amount_cents: number;
  month_count: number;
  month_amount_cents: number;
  average_amount_cents: number;
}
