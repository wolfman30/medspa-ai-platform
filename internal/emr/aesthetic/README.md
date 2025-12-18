# Aesthetic Record (AR) Shadow Scheduler

There is no publicly documented, general-purpose “book an appointment” API for Aesthetic Record that we can rely on during early onboarding. This package implements a **shadow scheduler**:

- A local cache of availability (“free slots”) plus locally-created reservations/appointments.
- A periodic sync (default **every 30 minutes**) that refreshes the cache from an upstream schedule source.

## Why the upstream is modeled like Nextech Select (FHIR)

Aesthetic Record is part of the Nextech ecosystem, and the most complete public request/response examples we can cite for scheduling are in the **Nextech Select API public docs**, which use **FHIR STU3** resources like `Slot` and `Appointment`.

This package’s mocked upstream parsing and JSON fixtures are based on those examples:

- Nextech Select API public docs: `https://nextechsystems.github.io/selectapidocspub/`
- FHIR STU3 spec (Slot / Appointment): `http://hl7.org/fhir/STU3/`

Fixture used in unit tests:

- `internal/emr/aesthetic/testdata/select_slot_bundle.json` (derived from the Slot “Sample” bundle in the public Select API docs)

## Components

- `Client` (`internal/emr/aesthetic/client.go`): Implements `internal/emr.Client` backed by an in-memory shadow schedule.
- `SelectAPIClient` (`internal/emr/aesthetic/selectapi.go`): HTTP client that fetches Slot bundles shaped like Nextech Select’s `/slot` endpoint and converts them into `emr.Slot`.
- `SyncService` (`internal/emr/aesthetic/syncer.go`): Runs an initial sync and then syncs on an interval (default 30m).

## Configuration (env vars)

Loaded via `internal/config/config.go` and wired in `internal/app/bootstrap/conversation.go`.

- `AESTHETIC_RECORD_CLINIC_ID` (required to enable the shadow EMR adapter)
- `AESTHETIC_RECORD_PROVIDER_ID` (optional; used for upstream slot filtering)
- `AESTHETIC_RECORD_SELECT_BASE_URL` (optional; upstream base URL, e.g. `https://select.nextech-api.com/api`)
- `AESTHETIC_RECORD_SELECT_BEARER_TOKEN` (optional; upstream bearer token)
- `AESTHETIC_RECORD_SHADOW_SYNC_ENABLED` (default `false`)
- `AESTHETIC_RECORD_SYNC_INTERVAL` (default `30m`)
- `AESTHETIC_RECORD_SYNC_WINDOW_DAYS` (default `7`)
- `AESTHETIC_RECORD_SYNC_DURATION_MINS` (default `30`)

## Notes / Limitations (current)

- The shadow schedule is **in-memory** per process. On restart, availability is repopulated on the next sync.
- Real Aesthetic Record credentials/contracted access are still required for true “book in AR” workflows; until then, local `CreateAppointment` reserves slots only in the shadow schedule.

