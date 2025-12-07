#!/usr/bin/env python3
"""Quick debug script to test Twilio webhook with minimal payload"""

import requests

payload = {
    "MessageSid": "SM123test",
    "AccountSid": "AC123test", 
    "From": "+19378962713",
    "To": "+18662894911",
    "Body": "test"
}

print("Sending to: http://localhost:8080/messaging/twilio/webhook")
print(f"Payload: {payload}")
print()

resp = requests.post(
    "http://localhost:8080/messaging/twilio/webhook",
    data=payload,
    headers={"Content-Type": "application/x-www-form-urlencoded"}
)

print(f"Status: {resp.status_code}")
print(f"Response: {resp.text}")
