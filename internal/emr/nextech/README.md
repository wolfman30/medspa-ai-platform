# Nextech EMR Integration

This package provides integration with Nextech EMR systems using their FHIR STU 3 API.

## Features

- ✅ OAuth 2.0 client credentials authentication
- ✅ FHIR STU 3 (3.0.1) compliant API calls
- ✅ Appointment booking and management
- ✅ Patient creation and search
- ✅ Availability/slot querying
- ✅ Automatic token refresh
- ✅ Rate limit awareness (20 req/sec per endpoint)

## Setup

### 1. Register at Nextech Developer Portal

1. Go to [https://www.nextech.com/developers-portal](https://www.nextech.com/developers-portal)
2. Create an account and register your application
3. Obtain OAuth 2.0 credentials:
   - Client ID
   - Client Secret
4. Note your API base URL (sandbox vs production)

### 2. Environment Configuration

Add to your `.env`:

```bash
# Nextech Configuration
NEXTECH_BASE_URL=https://api-sandbox.nextech.com  # or production URL
NEXTECH_CLIENT_ID=your-client-id-here
NEXTECH_CLIENT_SECRET=your-client-secret-here
```

### 3. Usage Example

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/wolfman30/medspa-ai-platform/internal/emr"
    "github.com/wolfman30/medspa-ai-platform/internal/emr/nextech"
)

func main() {
    // Create Nextech client
    client, err := nextech.New(nextech.Config{
        BaseURL:      "https://api-sandbox.nextech.com",
        ClientID:     "your-client-id",
        ClientSecret: "your-client-secret",
        Timeout:      30 * time.Second,
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Search for existing patient
    patients, err := client.SearchPatients(ctx, emr.PatientSearchQuery{
        Phone: "+15551234567",
    })
    if err != nil {
        log.Fatal(err)
    }

    var patientID string
    if len(patients) > 0 {
        patientID = patients[0].ID
        log.Printf("Found existing patient: %s %s", patients[0].FirstName, patients[0].LastName)
    } else {
        // Create new patient
        patient, err := client.CreatePatient(ctx, emr.Patient{
            FirstName:   "Jane",
            LastName:    "Doe",
            Email:       "jane.doe@example.com",
            Phone:       "+15551234567",
            DateOfBirth: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
            Gender:      "female",
            Address: emr.Address{
                Line1:      "123 Main St",
                City:       "Seattle",
                State:      "WA",
                PostalCode: "98101",
                Country:    "US",
            },
        })
        if err != nil {
            log.Fatal(err)
        }
        patientID = patient.ID
        log.Printf("Created new patient: %s", patientID)
    }

    // Get available appointment slots
    slots, err := client.GetAvailability(ctx, emr.AvailabilityRequest{
        ClinicID:     "clinic-123",
        ProviderID:   "provider-456",
        StartDate:    time.Now().Add(24 * time.Hour),
        EndDate:      time.Now().Add(7 * 24 * time.Hour),
        DurationMins: 30,
    })
    if err != nil {
        log.Fatal(err)
    }

    if len(slots) == 0 {
        log.Println("No available slots found")
        return
    }

    // Book the first available slot
    slot := slots[0]
    appointment, err := client.CreateAppointment(ctx, emr.AppointmentRequest{
        ClinicID:    "clinic-123",
        PatientID:   patientID,
        ProviderID:  slot.ProviderID,
        SlotID:      slot.ID,
        StartTime:   slot.StartTime,
        EndTime:     slot.EndTime,
        ServiceType: "consultation",
        Notes:       "Initial consultation - booked via AI platform",
        Status:      "booked",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Appointment created: %s at %s", appointment.ID, appointment.StartTime)
}
```

## API Reference

### Client Interface

The Nextech client implements the `emr.Client` interface:

```go
type Client interface {
    GetAvailability(ctx, req) ([]Slot, error)
    CreateAppointment(ctx, req) (*Appointment, error)
    GetAppointment(ctx, appointmentID) (*Appointment, error)
    CancelAppointment(ctx, appointmentID) error
    CreatePatient(ctx, patient) (*Patient, error)
    GetPatient(ctx, patientID) (*Patient, error)
    SearchPatients(ctx, query) ([]Patient, error)
}
```

### FHIR Endpoints Used

| Method | FHIR Endpoint | Purpose |
|--------|---------------|---------|
| `GetAvailability` | `GET /Slot?schedule={id}&start={start}&end={end}&status=free` | Query available appointment slots |
| `CreateAppointment` | `POST /Appointment` | Book a new appointment |
| `GetAppointment` | `GET /Appointment/{id}` | Retrieve appointment details |
| `CancelAppointment` | `PUT /Appointment/{id}` | Cancel appointment (set status=cancelled) |
| `CreatePatient` | `POST /Patient` | Create new patient record |
| `GetPatient` | `GET /Patient/{id}` | Retrieve patient details |
| `SearchPatients` | `GET /Patient?telecom={phone}&email={email}&name={name}` | Search for patients |

## Authentication

The client uses OAuth 2.0 client credentials flow:

1. On first API call, client requests access token from `/connect/token`
2. Access token is cached and reused for subsequent requests
3. Token is automatically refreshed when it expires (5-minute buffer)
4. All API requests include `Authorization: Bearer {token}` header

**Scopes requested:**
- `patient/*.read` - Read patient data
- `patient/*.write` - Write patient data
- `appointment/*.read` - Read appointment data
- `appointment/*.write` - Write appointment data
- `slot/*.read` - Read availability slots

## Rate Limiting

Nextech enforces **20 requests per second per endpoint**. The client does not currently implement rate limiting, so consider adding:

```go
import "golang.org/x/time/rate"

limiter := rate.NewLimiter(rate.Limit(20), 1) // 20 req/sec with burst of 1
limiter.Wait(ctx) // Before each API call
```

## Error Handling

All methods return errors that can be inspected:

```go
appointment, err := client.CreateAppointment(ctx, req)
if err != nil {
    if strings.Contains(err.Error(), "authentication failed") {
        // OAuth credentials issue
    } else if strings.Contains(err.Error(), "API error (status 404)") {
        // Resource not found
    } else if strings.Contains(err.Error(), "API error (status 400)") {
        // Invalid request (check FHIR validation)
    }
    // Handle error
}
```

## Testing

Run tests:

```bash
go test ./internal/emr/nextech -v
```

Mock server tests are included that verify:
- OAuth authentication flow
- Patient search
- Appointment creation
- FHIR resource parsing

## Production Checklist

Before going live with Nextech integration:

- [ ] Obtain production API credentials (not sandbox)
- [ ] Update `NEXTECH_BASE_URL` to production endpoint
- [ ] Test with real patient data (with consent)
- [ ] Verify rate limiting doesn't cause issues
- [ ] Implement error logging and alerts
- [ ] Add retry logic for transient failures
- [ ] Monitor API usage and costs
- [ ] Ensure HIPAA compliance in data handling

## Troubleshooting

### "authentication failed" errors

- Verify `NEXTECH_CLIENT_ID` and `NEXTECH_CLIENT_SECRET` are correct
- Check that credentials are for the correct environment (sandbox vs production)
- Ensure OAuth scopes match what's registered in developer portal

### "API error (status 400)" errors

- Check request data matches FHIR STU 3 format
- Verify required fields are populated
- Review Nextech API documentation for specific resource requirements

### No slots returned from GetAvailability

- Verify `ProviderID` (schedule reference) is correct
- Check date range is in the future
- Ensure provider has availability configured in Nextech
- Verify `status=free` filter is appropriate

## Resources

- [Nextech Developer Portal](https://www.nextech.com/developers-portal)
- [Nextech Select API Docs](https://nextechsystems.github.io/selectapidocspub/)
- [FHIR STU 3 Specification](http://hl7.org/fhir/STU3/)
- [Nextech GitHub](https://github.com/NextechSystems)
