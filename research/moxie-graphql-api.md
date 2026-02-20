# Moxie GraphQL API Reference

**Endpoint**: `https://graphql.joinmoxie.com/v1/graphql`  
**Auth**: None required for public booking queries  
**Introspection**: Blocked  
**Discovery method**: Field probing via error messages (Feb 20, 2026)

## Queries

### availableTimeSlots

Returns available appointment slots for a service at a med spa.

**Arguments** (query-level):
| Arg | Type | Required | Notes |
|-----|------|----------|-------|
| medspaId | ID! | ✅ | Moxie medspa numeric ID |
| startDate | Date! | ✅ | Format: "YYYY-MM-DD" |
| endDate | Date! | ✅ | Max ~3 months out |
| services | [CheckAvailabilityAppointmentServiceInput!]! | ✅ | Array of service specs |

**CheckAvailabilityAppointmentServiceInput fields**:
| Field | Type | Required | Notes |
|-------|------|----------|-------|
| serviceMenuItemId | String | ✅ | Moxie service menu item ID |
| noPreference | Boolean | ✅ | `true` = any provider; `false` = use `providerId` |
| order | Int | ✅ | 1-based ordering for multi-service bookings |
| providerId | String | ❌ | Provider's `userMedspaId` (NOT the provider table ID). Required when `noPreference=false` |

**NOT valid fields**: `providerUserMedspaId`, `userMedspaId`, `staffId`, `duration`, `quantity`, `addOnServiceMenuItemIds`, `addOns`, `selectedDate`, `clientId`, `locationId`, `roomId`, `appointmentTypeId`, `categoryId`, `notes`

**Response**: `TimeSlotType`
| Field | Type |
|-------|------|
| start | String (ISO 8601 with offset, e.g., "2026-02-21T13:00:00-05:00") |
| end | String (ISO 8601 with offset) |

**NOT on TimeSlotType**: `providerId`, `providerName`, `provider`, `staffName`, `staffId`, `id`, `duration`, `serviceMenuItemId`, `status`

**Key findings**:
- Slots include timezone offset (e.g., `-05:00` for EST)
- `noPreference: false` with no `providerId` → returns 0 slots
- `noPreference: true` → returns all providers' slots (cannot distinguish which provider)
- `providerId` uses `userMedspaId` value (e.g., "38627" for Gale at Forever 22), NOT the provider table ID (e.g., "34371" returns 0)

## Mutations

### createAppointmentByClient

Creates a booking. Used after Stripe payment for deposit-required services.

**Arguments** (mutation-level):
| Arg | Type | Required |
|-----|------|----------|
| medspaId | ID! | ✅ |
| firstName | String! | ✅ |
| lastName | String! | ✅ |
| email | String! | ✅ |
| phone | String! | ✅ |
| note | String! | ✅ (can be empty) |
| services | [CreateAppointmentServiceInput!]! | ✅ |
| bookingFlow | PublicBookingFlowTypeEnum | ❌ |
| isNewClient | Boolean | ❌ |
| noPreferenceProviderUsed | Boolean | ❌ |

**CreateAppointmentServiceInput fields**:
| Field | Type | Required | Notes |
|-------|------|----------|-------|
| serviceMenuItemId | String | ✅ | |
| providerId | String | ✅ | Provider's `userMedspaId` |
| startTime | String | ✅ | ISO 8601 datetime |
| endTime | String | ❌ | ISO 8601 datetime |

**NOT valid**: `providerUserMedspaId`, `userMedspaId`, `start`, `date`, `time`, `duration`, `quantity`, `addOns`

**Response**:
| Field | Type |
|-------|------|
| ok | Boolean |
| message | String |
| clientAccessToken | String |
| scheduledAppointment.id | String |

**bookingFlow enum values** (known):
- `MAIA_BOOKING` — bypasses deposit requirement (used for API bookings after external payment)

## Forever 22 Reference Data

| Entity | Value |
|--------|-------|
| medspaId | 1264 |
| Lip Filler serviceMenuItemId | 47693 |
| Brandi Sesock provider ID (for API) | 33950 (⚠️ verify — may need userMedspaId) |
| Gale Tesar userMedspaId (for API) | 38627 |
| Gale Tesar provider table ID (NOT for API) | 34371 |
