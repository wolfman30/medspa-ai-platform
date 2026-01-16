#!/usr/bin/env python3
"""
View SMS conversations for a clinic.

Usage:
    python scripts/view-conversations.py [--org ORG_ID] [--phone PHONE] [--limit LIMIT]

Environment variables required:
    COGNITO_USERNAME - Your Cognito username (email)
    COGNITO_PASSWORD - Your Cognito password

Or provide a token directly:
    API_TOKEN - Bearer token if already authenticated
"""

import argparse
import json
import os
import sys
from datetime import datetime

try:
    import requests
except ImportError:
    print("Error: requests library required. Install with: pip install requests")
    sys.exit(1)

try:
    import boto3
except ImportError:
    boto3 = None

API_BASE = os.getenv("API_BASE", "https://api-dev.aiwolfsolutions.com")
COGNITO_USER_POOL_ID = "us-east-1_eGSeUyPdg"
COGNITO_CLIENT_ID = os.getenv("COGNITO_CLIENT_ID", "")
DEFAULT_ORG = "brilliant-aesthetics"


def get_cognito_token(username: str, password: str) -> str:
    """Authenticate with Cognito and return access token."""
    if not boto3:
        print("Error: boto3 required for Cognito auth. Install with: pip install boto3")
        sys.exit(1)

    client = boto3.client("cognito-idp", region_name="us-east-1")

    # Get client ID if not provided
    client_id = COGNITO_CLIENT_ID
    if not client_id:
        # List user pool clients to find the app client
        try:
            response = client.list_user_pool_clients(UserPoolId=COGNITO_USER_POOL_ID, MaxResults=10)
            if response.get("UserPoolClients"):
                client_id = response["UserPoolClients"][0]["ClientId"]
        except Exception as e:
            print(f"Error listing Cognito clients: {e}")
            sys.exit(1)

    if not client_id:
        print("Error: Could not determine Cognito client ID")
        sys.exit(1)

    try:
        response = client.initiate_auth(
            AuthFlow="USER_PASSWORD_AUTH",
            ClientId=client_id,
            AuthParameters={
                "USERNAME": username,
                "PASSWORD": password,
            },
        )
        return response["AuthenticationResult"]["AccessToken"]
    except Exception as e:
        print(f"Cognito authentication failed: {e}")
        sys.exit(1)


def get_auth_headers() -> dict:
    """Get authentication headers."""
    token = os.getenv("API_TOKEN")

    if not token:
        username = os.getenv("COGNITO_USERNAME")
        password = os.getenv("COGNITO_PASSWORD")

        if username and password:
            token = get_cognito_token(username, password)
        else:
            print("Error: Set API_TOKEN or COGNITO_USERNAME/COGNITO_PASSWORD")
            sys.exit(1)

    return {"Authorization": f"Bearer {token}"}


def list_conversations(org_id: str, phone: str = None, limit: int = 20) -> list:
    """List conversations for an organization."""
    headers = get_auth_headers()

    params = {"page_size": limit}
    if phone:
        params["phone"] = phone

    url = f"{API_BASE}/admin/orgs/{org_id}/conversations"
    response = requests.get(url, headers=headers, params=params)

    if response.status_code != 200:
        print(f"Error: {response.status_code} - {response.text}")
        return []

    data = response.json()
    return data.get("conversations", [])


def get_conversation_detail(org_id: str, conversation_id: str) -> dict:
    """Get detailed conversation with messages."""
    headers = get_auth_headers()

    url = f"{API_BASE}/admin/orgs/{org_id}/conversations/{conversation_id}"
    response = requests.get(url, headers=headers)

    if response.status_code != 200:
        print(f"Error: {response.status_code} - {response.text}")
        return {}

    return response.json()


def format_timestamp(ts: str) -> str:
    """Format ISO timestamp to readable format."""
    try:
        dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
        return dt.strftime("%Y-%m-%d %H:%M:%S")
    except:
        return ts


def print_conversation_list(conversations: list):
    """Print conversation list in a readable format."""
    if not conversations:
        print("No conversations found.")
        return

    print(f"\n{'='*80}")
    print(f"{'Phone':<15} {'Messages':<10} {'Last Message':<20} {'Status':<10}")
    print(f"{'='*80}")

    for conv in conversations:
        phone = conv.get("customer_phone", "")[-10:] if conv.get("customer_phone") else "Unknown"
        msg_count = conv.get("message_count", 0)
        last_msg = format_timestamp(conv.get("last_message_at", "")) if conv.get("last_message_at") else "N/A"
        status = conv.get("status", "unknown")

        print(f"{phone:<15} {msg_count:<10} {last_msg:<20} {status:<10}")

    print(f"{'='*80}")
    print(f"Total: {len(conversations)} conversations\n")


def print_conversation_detail(detail: dict):
    """Print detailed conversation with messages."""
    if not detail:
        print("Conversation not found.")
        return

    print(f"\n{'='*80}")
    print(f"Conversation: {detail.get('customer_phone', 'Unknown')}")
    print(f"Status: {detail.get('status', 'unknown')}")
    print(f"Started: {format_timestamp(detail.get('started_at', ''))}")
    if detail.get("last_message_at"):
        print(f"Last Message: {format_timestamp(detail.get('last_message_at', ''))}")
    print(f"{'='*80}\n")

    messages = detail.get("messages", [])
    if not messages:
        print("No messages found.")
        return

    for msg in messages:
        role = msg.get("role", "unknown")
        content = msg.get("content", "")
        timestamp = format_timestamp(msg.get("timestamp", ""))

        role_label = "CUSTOMER" if role == "user" else "AI" if role == "assistant" else role.upper()

        print(f"[{timestamp}] {role_label}:")
        print(f"  {content}")
        print()


def main():
    parser = argparse.ArgumentParser(description="View SMS conversations")
    parser.add_argument("--org", default=DEFAULT_ORG, help=f"Organization ID (default: {DEFAULT_ORG})")
    parser.add_argument("--phone", help="Filter by phone number")
    parser.add_argument("--limit", type=int, default=20, help="Number of conversations to show (default: 20)")
    parser.add_argument("--detail", help="Show detailed view of a specific conversation ID")
    parser.add_argument("--all", action="store_true", help="Show all conversations with messages")

    args = parser.parse_args()

    print(f"Fetching conversations for: {args.org}")
    print(f"API: {API_BASE}")

    if args.detail:
        # Show specific conversation
        detail = get_conversation_detail(args.org, args.detail)
        print_conversation_detail(detail)
    elif args.all:
        # Show all conversations with messages
        conversations = list_conversations(args.org, args.phone, args.limit)
        for conv in conversations:
            conv_id = conv.get("id")
            if conv_id:
                detail = get_conversation_detail(args.org, conv_id)
                print_conversation_detail(detail)
                print("\n" + "-"*80 + "\n")
    else:
        # Show conversation list
        conversations = list_conversations(args.org, args.phone, args.limit)
        print_conversation_list(conversations)

        if conversations:
            print("To view a specific conversation:")
            print(f"  python scripts/view-conversations.py --detail <conversation_id>")
            print("\nTo view all conversations with messages:")
            print(f"  python scripts/view-conversations.py --all")


if __name__ == "__main__":
    main()
