package aesthetic

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

var (
	errAppointmentNotFound = errors.New("aesthetic: appointment not found")
	errPatientNotFound     = errors.New("aesthetic: patient not found")
	errSlotNotFound        = errors.New("aesthetic: slot not found")
	errSlotUnavailable     = errors.New("aesthetic: slot unavailable")
)

type appointmentRecord struct {
	appointment emr.Appointment
	slot        emr.Slot
}

type memoryStore struct {
	mu sync.RWMutex

	slotsByClinic      map[string]map[string]emr.Slot
	lastSyncByClinic   map[string]time.Time
	appointmentsByID   map[string]appointmentRecord
	patientsByID       map[string]emr.Patient
	patientIDsByPhone  map[string][]string
	patientIDsByEmail  map[string][]string
	patientIDsByNameLC map[string][]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		slotsByClinic:      make(map[string]map[string]emr.Slot),
		lastSyncByClinic:   make(map[string]time.Time),
		appointmentsByID:   make(map[string]appointmentRecord),
		patientsByID:       make(map[string]emr.Patient),
		patientIDsByPhone:  make(map[string][]string),
		patientIDsByEmail:  make(map[string][]string),
		patientIDsByNameLC: make(map[string][]string),
	}
}

func (s *memoryStore) replaceSlots(clinicID string, asOf time.Time, slots []emr.Slot) {
	if strings.TrimSpace(clinicID) == "" {
		return
	}

	slotMap := make(map[string]emr.Slot, len(slots))
	for _, slot := range slots {
		if strings.TrimSpace(slot.ID) == "" {
			continue
		}
		slotMap[slot.ID] = slot
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.slotsByClinic[clinicID] = slotMap
	s.lastSyncByClinic[clinicID] = asOf

	s.removeConflictingSlotsLocked(clinicID)
}

func (s *memoryStore) removeConflictingSlotsLocked(clinicID string) {
	slotMap := s.slotsByClinic[clinicID]
	if len(slotMap) == 0 {
		return
	}

	for slotID, slot := range slotMap {
		for _, rec := range s.appointmentsByID {
			appt := rec.appointment
			if appt.ClinicID != clinicID {
				continue
			}
			if isCancelledStatus(appt.Status) {
				continue
			}
			if overlaps(slot.StartTime, slot.EndTime, appt.StartTime, appt.EndTime) {
				delete(slotMap, slotID)
				break
			}
		}
	}
}

func (s *memoryStore) listSlots(clinicID string, start, end time.Time) []emr.Slot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	slotMap := s.slotsByClinic[clinicID]
	if len(slotMap) == 0 {
		return nil
	}

	out := make([]emr.Slot, 0, len(slotMap))
	for _, slot := range slotMap {
		if !start.IsZero() && slot.EndTime.Before(start) {
			continue
		}
		if !end.IsZero() && slot.StartTime.After(end) {
			continue
		}
		out = append(out, slot)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].StartTime.Before(out[j].StartTime)
	})

	return out
}

func (s *memoryStore) lastSync(clinicID string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSyncByClinic[clinicID]
}

func (s *memoryStore) createPatient(patient emr.Patient, now time.Time) (*emr.Patient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(patient.ID) == "" {
		patient.ID = uuid.NewString()
	}

	if patient.CreatedAt.IsZero() {
		patient.CreatedAt = now
	}
	patient.UpdatedAt = now

	s.patientsByID[patient.ID] = patient

	if phone := normalizePhone(patient.Phone); phone != "" {
		s.patientIDsByPhone[phone] = appendUnique(s.patientIDsByPhone[phone], patient.ID)
	}
	if email := normalizeEmail(patient.Email); email != "" {
		s.patientIDsByEmail[email] = appendUnique(s.patientIDsByEmail[email], patient.ID)
	}
	if name := normalizeName(patient.FirstName, patient.LastName); name != "" {
		s.patientIDsByNameLC[name] = appendUnique(s.patientIDsByNameLC[name], patient.ID)
	}

	cpy := patient
	return &cpy, nil
}

func (s *memoryStore) getPatient(patientID string) (*emr.Patient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	patient, ok := s.patientsByID[patientID]
	if !ok {
		return nil, errPatientNotFound
	}
	cpy := patient
	return &cpy, nil
}

func (s *memoryStore) searchPatients(query emr.PatientSearchQuery) ([]emr.Patient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []string
	if phone := normalizePhone(query.Phone); phone != "" {
		ids = append(ids, s.patientIDsByPhone[phone]...)
	}
	if email := normalizeEmail(query.Email); email != "" {
		ids = append(ids, s.patientIDsByEmail[email]...)
	}
	if name := normalizeName(query.FirstName, query.LastName); name != "" {
		ids = append(ids, s.patientIDsByNameLC[name]...)
	}

	ids = uniqueStrings(ids)

	out := make([]emr.Patient, 0, len(ids))
	for _, id := range ids {
		patient, ok := s.patientsByID[id]
		if !ok {
			continue
		}
		out = append(out, patient)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].LastName == out[j].LastName {
			return out[i].FirstName < out[j].FirstName
		}
		return out[i].LastName < out[j].LastName
	})

	return out, nil
}

func (s *memoryStore) createAppointment(clinicID string, now time.Time, req emr.AppointmentRequest) (*emr.Appointment, error) {
	if strings.TrimSpace(req.SlotID) == "" {
		return nil, fmtError("aesthetic: SlotID is required")
	}
	if strings.TrimSpace(req.PatientID) == "" {
		return nil, fmtError("aesthetic: PatientID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	slotMap := s.slotsByClinic[clinicID]
	slot, ok := slotMap[req.SlotID]
	if !ok {
		return nil, errSlotNotFound
	}
	if slot.Status != "" && slot.Status != "free" {
		return nil, errSlotUnavailable
	}

	start := slot.StartTime
	end := slot.EndTime
	if !req.StartTime.IsZero() {
		start = req.StartTime
	}
	if !req.EndTime.IsZero() {
		end = req.EndTime
	}

	appointment := emr.Appointment{
		ID:           uuid.NewString(),
		ClinicID:     clinicID,
		PatientID:    req.PatientID,
		ProviderID:   firstNonEmpty(req.ProviderID, slot.ProviderID),
		ProviderName: slot.ProviderName,
		StartTime:    start,
		EndTime:      end,
		ServiceType:  firstNonEmpty(req.ServiceType, slot.ServiceType),
		Status:       firstNonEmpty(req.Status, "booked"),
		Notes:        req.Notes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	delete(slotMap, req.SlotID)

	rec := appointmentRecord{
		appointment: appointment,
		slot:        slot,
	}
	s.appointmentsByID[appointment.ID] = rec

	cpy := appointment
	return &cpy, nil
}

func (s *memoryStore) getAppointment(appointmentID string) (*emr.Appointment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.appointmentsByID[appointmentID]
	if !ok {
		return nil, errAppointmentNotFound
	}
	cpy := rec.appointment
	return &cpy, nil
}

func (s *memoryStore) cancelAppointment(now time.Time, appointmentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.appointmentsByID[appointmentID]
	if !ok {
		return errAppointmentNotFound
	}

	if !isCancelledStatus(rec.appointment.Status) {
		rec.appointment.Status = "cancelled"
		rec.appointment.UpdatedAt = now
		s.appointmentsByID[appointmentID] = rec
	}

	if strings.TrimSpace(rec.slot.ID) != "" {
		slotMap := s.slotsByClinic[rec.appointment.ClinicID]
		if slotMap == nil {
			slotMap = make(map[string]emr.Slot)
			s.slotsByClinic[rec.appointment.ClinicID] = slotMap
		}
		rec.slot.Status = "free"
		slotMap[rec.slot.ID] = rec.slot
	}

	return nil
}

func overlaps(aStart, aEnd, bStart, bEnd time.Time) bool {
	if aStart.IsZero() || aEnd.IsZero() || bStart.IsZero() || bEnd.IsZero() {
		return false
	}
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}

func isCancelledStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	return phone
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeName(first, last string) string {
	first = strings.ToLower(strings.TrimSpace(first))
	last = strings.ToLower(strings.TrimSpace(last))
	if first == "" && last == "" {
		return ""
	}
	return strings.TrimSpace(first + " " + last)
}

func appendUnique(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type fmtError string

func (e fmtError) Error() string { return string(e) }
