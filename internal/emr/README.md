# EMR Integration Package

This package provides a unified interface for integrating with various EMR (Electronic Medical Record) systems used by medical spas.

## Architecture

```
internal/emr/
├── types.go              # Shared EMR client interface + types
├── nextech/              # Nextech EMR implementation (FHIR STU 3)
│   ├── client.go         # Main client implementation
│   ├── fhir.go          # FHIR resource models + parsers
│   ├── client_test.go   # Tests
│   └── README.md        # Nextech-specific documentation
├── boulevard/            # TODO: Boulevard implementation
└── aesthetic/            # TODO: Aesthetic Record implementation
```

## Unified Interface

All EMR clients implement the same `emr.Client` interface:

```go
type Client interface {
    // Availability
    GetAvailability(ctx context.Context, req AvailabilityRequest) ([]Slot, error)

    // Appointments
    CreateAppointment(ctx context.Context, req AppointmentRequest) (*Appointment, error)
    GetAppointment(ctx context.Context, appointmentID string) (*Appointment, error)
    CancelAppointment(ctx context.Context, appointmentID string) error

    // Patients
    CreatePatient(ctx context.Context, patient Patient) (*Patient, error)
    GetPatient(ctx context.Context, patientID string) (*Patient, error)
    SearchPatients(ctx context.Context, query PatientSearchQuery) ([]Patient, error)
}
```

## Supported EMRs

| EMR | Market Share | Status | API Access | Directory |
|-----|--------------|--------|------------|-----------|
| **Nextech** | 15% | ✅ Implemented | Public API | [`nextech/`](nextech/) |
| **Boulevard** | 25% | 🚧 Planned | Enterprise only | `boulevard/` |
| **Aesthetic Record** | 30% | 🚧 Planned | Partnership required | `aesthetic/` |

---

## Quick Start

### 1. Choose Your EMR

Based on your target medspa's EMR system:

```go
import (
    "github.com/wolfman30/medspa-ai-platform/internal/emr"
    "github.com/wolfman30/medspa-ai-platform/internal/emr/nextech"
    // "github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
    // "github.com/wolfman30/medspa-ai-platform/internal/emr/aesthetic"
)

// Create Nextech client
var emrClient emr.Client
emrClient, err := nextech.New(nextech.Config{
    BaseURL:      os.Getenv("NEXTECH_BASE_URL"),
    ClientID:     os.Getenv("NEXTECH_CLIENT_ID"),
    ClientSecret: os.Getenv("NEXTECH_CLIENT_SECRET"),
})
```

### 2. Use the Unified Interface

Once you have an `emr.Client`, the code is the same regardless of EMR:

```go
// Search for patient
patients, err := emrClient.SearchPatients(ctx, emr.PatientSearchQuery{
    Phone: "+15551234567",
})

// Get availability
slots, err := emrClient.GetAvailability(ctx, emr.AvailabilityRequest{
    ClinicID:     "clinic-123",
    StartDate:    time.Now().Add(24 * time.Hour),
    EndDate:      time.Now().Add(7 * 24 * time.Hour),
    DurationMins: 30,
})

// Book appointment
appointment, err := emrClient.CreateAppointment(ctx, emr.AppointmentRequest{
    PatientID:  patients[0].ID,
    ProviderID: slots[0].ProviderID,
    SlotID:     slots[0].ID,
    StartTime:  slots[0].StartTime,
    EndTime:    slots[0].EndTime,
    Status:     "booked",
})
```

---

## Data Models

### Slot (Availability)

```go
type Slot struct {
    ID           string    // EMR-specific slot identifier
    ProviderID   string    // Provider offering this slot
    ProviderName string    // Human-readable provider name
    StartTime    time.Time // Slot start time
    EndTime      time.Time // Slot end time
    Status       string    // "free", "busy", "busy-unavailable"
    ServiceType  string    // Type of service this slot is for
}
```

### Appointment

```go
type Appointment struct {
    ID           string    // EMR-specific appointment identifier
    ClinicID     string    // Clinic/location identifier
    PatientID    string    // Patient identifier
    ProviderID   string    // Provider identifier
    ProviderName string    // Human-readable provider name
    StartTime    time.Time // Appointment start time
    EndTime      time.Time // Appointment end time
    ServiceType  string    // Type of service
    Status       string    // "booked", "arrived", "fulfilled", "cancelled"
    Notes        string    // Appointment notes
    CreatedAt    time.Time // When appointment was created
    UpdatedAt    time.Time // When appointment was last updated
}
```

### Patient

```go
type Patient struct {
    ID          string    // EMR-specific patient identifier
    FirstName   string    // Patient first name
    LastName    string    // Patient last name
    Email       string    // Patient email
    Phone       string    // Patient phone (E.164 format)
    DateOfBirth time.Time // Patient date of birth
    Gender      string    // "male", "female", "other", "unknown"
    Address     Address   // Patient address
    CreatedAt   time.Time // When patient record was created
    UpdatedAt   time.Time // When patient record was last updated
}
```

---

## Integration Strategy

### For First Client (MVP)

1. **Register with Nextech** (fastest, public API)
   - Go to [nextech.com/developers-portal](https://www.nextech.com/developers-portal)
   - Get OAuth credentials
   - Start using immediately

2. **Qualify Your First Client**
   - Ask: "Which EMR do you use?"
   - If Nextech → ✅ Ready to go!
   - If Boulevard → Email support@blvd.co (wait 1-2 weeks)
   - If Aesthetic Record → Contact sales (wait 2-4 weeks)

### For Scaling (10+ Clients)

Once you have multiple clients using different EMRs, implement the multi-EMR factory pattern:

```go
package emr

type Factory struct {
    nextech  *nextech.Client
    boulevard *boulevard.Client
    aesthetic *aesthetic.Client
}

func (f *Factory) GetClientForClinic(clinicID string) (Client, error) {
    // Look up clinic's EMR type from database
    emrType := f.lookupEMRType(clinicID)

    switch emrType {
    case "nextech":
        return f.nextech, nil
    case "boulevard":
        return f.boulevard, nil
    case "aesthetic_record":
        return f.aesthetic, nil
    default:
        return nil, fmt.Errorf("unsupported EMR: %s", emrType)
    }
}
```

---

## Testing

Run all EMR integration tests:

```bash
go test ./internal/emr/... -v
```

Run tests for specific EMR:

```bash
go test ./internal/emr/nextech -v
```

---

## Next Steps

### Immediate (For MVP)

1. ✅ **Nextech Integration** - Complete!
2. ⬜ **Register at Nextech Developer Portal** - Get OAuth credentials
3. ⬜ **Test with Sandbox** - Verify integration works
4. ⬜ **Wire into Conversation Service** - Connect EMR to AI booking flow

### Short-Term (First 3 Months)

1. ⬜ **Boulevard Integration** - Contact support@blvd.co
2. ⬜ **Aesthetic Record Integration** - Contact AR sales
3. ⬜ **Add Caching Layer** - Cache availability queries (5-min TTL)
4. ⬜ **Add Rate Limiting** - Respect EMR API limits

### Long-Term (After 10+ Clients)

1. ⬜ **EMR Sync Worker** - Background job to keep data in sync
2. ⬜ **Webhook Support** - Receive real-time updates from EMRs
3. ⬜ **Advanced Scheduling** - Multi-provider, recurring appointments
4. ⬜ **Additional EMRs** - PatientNow, Mangomint, Nextech, etc.

---

## Production Checklist

Before going live with EMR integration:

- [ ] Obtained production API credentials (not sandbox)
- [ ] Tested with real clinic data (with appropriate consent)
- [ ] Implemented error handling and logging
- [ ] Added retry logic for transient failures
- [ ] Set up monitoring and alerts
- [ ] Documented clinic onboarding process
- [ ] Verified HIPAA compliance
- [ ] Load tested with expected request volume
- [ ] Created runbook for common issues

---

## Troubleshooting

### "No EMR client available for clinic"

- Verify clinic has been properly onboarded
- Check EMR type is correctly stored in database
- Ensure EMR credentials are configured

### "Patient already exists" errors

- Always use `SearchPatients()` before `CreatePatient()`
- Some EMRs deduplicate by phone number
- Check EMR-specific patient matching logic

### Slow availability queries

- Implement caching (5-minute TTL recommended)
- Reduce date range in queries
- Consider pre-fetching common provider schedules

### Rate limit errors

- Implement exponential backoff
- Add request queuing for high-volume operations
- Consider dedicated API quotas from EMR providers

---

## Resources

- [Nextech Integration Docs](nextech/README.md)
- [FHIR STU 3 Specification](http://hl7.org/fhir/STU3/)
- [Boulevard Developer Portal](https://developers.joinblvd.com/)
- [Aesthetic Record Website](https://www.aestheticrecord.com/)
