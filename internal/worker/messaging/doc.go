// Package messagingworker contains background workers for the messaging
// subsystem. It handles:
//
//   - SMS retry delivery via [RetrySender] (exponential backoff, max 5 attempts).
//   - Hosted number order polling via [HostedPoller] (tracks LOA/porting status
//     until activation).
//
// Both workers run as long-lived goroutines started from the main application
// bootstrap and are cancelled via context on shutdown.
package messagingworker
