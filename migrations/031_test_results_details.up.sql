-- Add description and evidence columns to manual_test_results
ALTER TABLE manual_test_results
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evidence_urls TEXT[] NOT NULL DEFAULT '{}';

-- Seed detailed descriptions for all scenarios
UPDATE manual_test_results SET description = 'Call Forever 22 from your phone. Let it ring to voicemail. Wait for the AI text-back. Reply as a NEW patient wanting Botox, no time preference initially. Complete full flow: name → service → patient type → schedule preference → time slot selection → booking policies → Stripe deposit → confirmation SMS.' WHERE scenario_id = 'phone-1';

UPDATE manual_test_results SET description = 'Call Forever 22 from your phone. Let it ring to voicemail. Reply as a NEW patient wanting lip filler, specifically requesting provider Gale Tesar. Verify: AI asks for name first, then service, recognizes lip filler, asks patient type, then schedule. Availability should be filtered to Gale only. Complete through deposit and confirmation.' WHERE scenario_id = 'phone-2';

UPDATE manual_test_results SET description = 'Call Forever 22, reply as a NEW patient wanting any service. When asked about schedule preference, say something like "whenever works" or "I''m flexible." Verify: AI presents a spread of available times across multiple days without asking for a specific day/time preference. Complete through deposit.' WHERE scenario_id = 'phone-3';

UPDATE manual_test_results SET description = 'Call Forever 22, reply as a RETURNING patient (say "I''ve been here before" or "returning patient"). Verify: AI skips the new/returning patient question, does NOT ask for info it would only need from new patients, and moves efficiently to scheduling. Should still ask for name and service.' WHERE scenario_id = 'phone-4';

UPDATE manual_test_results SET description = 'Call Forever 22, ask about pricing (e.g., "How much is Botox?" or "What are your filler prices?"). Verify: AI provides pricing info from the clinic knowledge base, does NOT make up prices, and offers to help book an appointment. Check that prices match what''s configured in the knowledge base.' WHERE scenario_id = 'phone-5';

UPDATE manual_test_results SET description = 'Call Forever 22, ask "What services do you offer?" or "What treatments do you have?" Verify: AI provides a helpful summary of available services from the knowledge base, organized logically (not a raw dump of 46 services). Should offer to help book or provide more details on specific services.' WHERE scenario_id = 'phone-6';

UPDATE manual_test_results SET description = 'Mid-conversation, text "STOP" to the Forever 22 number. Verify: you get an opt-out confirmation and NO further messages are sent. Then text "START" to re-subscribe. Verify: you get a re-subscription confirmation and the AI resumes responding normally.' WHERE scenario_id = 'phone-7';

UPDATE manual_test_results SET description = 'Call the Brilliant Aesthetics Telnyx number. Complete a happy-path booking flow: new patient, any injectable service, through to deposit link. Verifies SMS delivery, AI responses, and booking flow work for a non-Forever-22 clinic.' WHERE scenario_id = 'smoke-1';

UPDATE manual_test_results SET description = 'Call the Lucy''s Laser Telnyx number. Complete a happy-path booking flow. Verifies multi-clinic SMS routing and service configuration for a clinic with 55 services and 5 providers.' WHERE scenario_id = 'smoke-2';

UPDATE manual_test_results SET description = 'Call the Adela Medical Spa Telnyx number. Complete a happy-path booking flow. Verifies multi-clinic SMS routing and service configuration for a clinic with 70+ services and 11 providers.' WHERE scenario_id = 'smoke-3';

UPDATE manual_test_results SET description = 'Automated E2E test suite run via CI (GitHub Actions post-deploy). 30 scenarios covering: happy path, multi-turn, service vocabulary, returning patient, service questions, medical liability, emergency, post-procedure, weight loss, provider preference, more-times, booking intent, diagnosis, treatment recommendations, no-area-question, SMS brevity, STOP/START, prompt injection, and more. Latest: 29/30 passing (98%).' WHERE scenario_id = 'e2e-suite';
