// Types for conversation viewer

export interface ConversationListItem {
  id: string;
  org_id: string;
  customer_phone: string;
  status: string;
  message_count: number;
  customer_message_count: number;
  ai_message_count: number;
  started_at: string;
  last_message_at?: string;
}

export interface ConversationsListResponse {
  conversations: ConversationListItem[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface MessageResponse {
  id: string;
  role: string;
  content: string;
  timestamp: string;
  from?: string;
  to?: string;
}

export interface ConversationMeta {
  total_messages: number;
  customer_messages: number;
  ai_messages: number;
  source: string;
}

export interface ConversationDetailResponse {
  id: string;
  org_id: string;
  customer_phone: string;
  status: string;
  started_at: string;
  last_message_at?: string;
  messages: MessageResponse[];
  metadata: ConversationMeta;
}
