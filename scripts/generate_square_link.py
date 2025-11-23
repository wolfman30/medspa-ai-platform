#!/usr/bin/env python3
import os
import json
import uuid
import urllib.request
import urllib.error

def load_env():
    """Load environment variables from .env file"""
    env_path = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), '.env')
    if os.path.exists(env_path):
        with open(env_path, 'r') as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith('#') and '=' in line:
                    key, value = line.split('=', 1)
                    os.environ[key] = value

def generate_link():
    load_env()
    
    access_token = os.environ.get('SQUARE_ACCESS_TOKEN')
    location_id = os.environ.get('SQUARE_LOCATION_ID')
    
    if not access_token or not location_id:
        print("Error: SQUARE_ACCESS_TOKEN or SQUARE_LOCATION_ID not found in .env")
        return

    url = "https://connect.squareup.com/v2/online-checkout/payment-links"
    
    payload = {
        "idempotency_key": str(uuid.uuid4()),
        "quick_pay": {
            "name": "MedSpa Consultation Deposit",
            "price_money": {
                "amount": 5000, # $50.00
                "currency": "USD"
            },
            "location_id": location_id
        },
        "checkout_options": {
            "redirect_url": "https://aiwolfsolutions.com/booking/confirmed",
            "ask_for_shipping_address": False
        }
    }

    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode('utf-8'),
        headers={
            "Authorization": f"Bearer {access_token}",
            "Content-Type": "application/json",
            "Square-Version": "2023-10-20"
        }
    )

    try:
        with urllib.request.urlopen(req) as response:
            data = json.loads(response.read().decode('utf-8'))
            payment_link = data.get('payment_link', {})
            long_url = payment_link.get('url')
            print("\n✅ SUCCESS! Here is your test payment link for Telnyx:")
            print("-" * 60)
            print(long_url)
            print("-" * 60)
            print("This link will charge $50.00 and redirect to your confirmation page.")
            
    except urllib.error.HTTPError as e:
        print(f"\n❌ Error calling Square API: {e.code}")
        print(e.read().decode('utf-8'))
    except Exception as e:
        print(f"\n❌ Error: {str(e)}")

if __name__ == "__main__":
    generate_link()
