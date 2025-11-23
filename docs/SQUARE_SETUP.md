# Square Payment Integration Setup

## Prerequisites

1.  **Square Developer Account**: You need a Square Developer account.
2.  **Application**: Create an application in the Square Developer Dashboard.
3.  **Credentials**: Obtain the following credentials from your application:
    *   `Access Token`
    *   `Application ID`
    *   `Location ID`
    *   `Webhook Signature Key`

## Configuration

The integration is configured using environment variables.

| Variable | Description | Required | Default |
| :--- | :--- | :--- | :--- |
| `SQUARE_ACCESS_TOKEN` | The OAuth access token for your Square application. | Yes | - |
| `SQUARE_LOCATION_ID` | The ID of the location to associate payments with. | Yes | - |
| `SQUARE_WEBHOOK_SIGNATURE_KEY` | The key used to verify webhook signatures. | Yes | - |
| `SQUARE_SUCCESS_URL` | Default URL to redirect to after successful payment. | Yes | - |
| `SQUARE_CANCEL_URL` | Default URL to redirect to after cancelled payment. | Yes | - |

## Webhooks

Configure your Square application to send webhooks to your API endpoint:

*   **URL**: `https://<your-api-domain>/webhooks/square`
*   **Events**:
    *   `payment.created`
    *   `payment.updated`

## API Usage

### Create Checkout Link

**Endpoint**: `POST /payments/checkout`

**Headers**:
- `X-Org-ID`: `<your-org-id>`

**Body**:
```json
{
  "lead_id": "uuid-of-lead",
  "amount_cents": 5000,
  "success_url": "https://example.com/success",
  "cancel_url": "https://example.com/cancel"
}
```

**Response**:
```json
{
  "checkout_url": "https://square.link/...",
  "provider": "square"
}
```
