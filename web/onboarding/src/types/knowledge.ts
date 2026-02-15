export interface StructuredKnowledge {
  org_id: string;
  version: number;
  sections: KnowledgeSections;
  updated_at: string;
}

export interface KnowledgeSections {
  services: ServiceSection;
  providers: ProviderSection;
  policies: PolicySection;
  custom?: CustomDoc[];
}

export interface ServiceSection {
  items: ServiceItem[];
}

export interface ServiceItem {
  id: string;
  name: string;
  category: string;
  price: string;
  price_type: 'fixed' | 'variable' | 'free' | 'starting_at';
  duration_minutes: number;
  description: string;
  provider_ids: string[];
  booking_id: string;
  aliases: string[];
  deposit_amount_cents: number;
  is_addon: boolean;
  order: number;
}

export interface ProviderSection {
  items: ProviderItem[];
}

export interface ProviderItem {
  id: string;
  name: string;
  title?: string;
  bio?: string;
  specialties?: string[];
  order: number;
}

export interface PolicySection {
  cancellation: string;
  deposit: string;
  age_requirement: string;
  terms_url?: string;
  booking_policies: string[];
  custom?: string[];
}

export interface CustomDoc {
  title: string;
  content: string;
}
