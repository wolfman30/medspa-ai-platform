#!/usr/bin/env python3
"""
Plaid Financial OS — AI Wolf Solutions
Read-only bank account integration for automated P&L tracking.

Usage:
  python3 plaid_client.py create-link-token     # Step 1: Generate link token for browser
  python3 plaid_client.py exchange <public_token> # Step 2: Exchange for access token
  python3 plaid_client.py balances               # Get current balances
  python3 plaid_client.py transactions [--days N] # Get recent transactions
  python3 plaid_client.py summary                # Weekly financial summary
  python3 plaid_client.py sandbox-test           # Test with sandbox (fake data)
"""

import argparse
import json
import os
import sys
from datetime import datetime, timedelta
from pathlib import Path

# Plaid API via stdlib (no external dependencies)
import urllib.request
import urllib.error

PLAID_CLIENT_ID = os.environ.get('PLAID_CLIENT_ID', '')
PLAID_SECRET = os.environ.get('PLAID_SECRET', '')
PLAID_ENV = os.environ.get('PLAID_ENV', 'sandbox')  # sandbox | development | production
PLAID_ACCESS_TOKEN = os.environ.get('PLAID_ACCESS_TOKEN', '')

BASE_URLS = {
    'sandbox': 'https://sandbox.plaid.com',
    'development': 'https://development.plaid.com',
    'production': 'https://production.plaid.com',
}

def base_url():
    return BASE_URLS.get(PLAID_ENV, BASE_URLS['sandbox'])

def plaid_request(endpoint, payload):
    """Make authenticated Plaid API request."""
    payload['client_id'] = PLAID_CLIENT_ID
    payload['secret'] = PLAID_SECRET
    data = json.dumps(payload).encode('utf-8')
    req = urllib.request.Request(
        f"{base_url()}{endpoint}",
        data=data,
        headers={'Content-Type': 'application/json'},
        method='POST'
    )
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        body = e.read().decode('utf-8')
        print(f"❌ Plaid API error ({e.code}): {body}", file=sys.stderr)
        sys.exit(1)


def create_link_token():
    """Create a link token for the Plaid Link UI."""
    data = plaid_request('/link/token/create', {
        'user': {'client_user_id': 'aiwolf-andrew'},
        'client_name': 'AI Wolf Financial OS',
        'products': ['transactions'],
        'country_codes': ['US'],
        'language': 'en',
    })
    token = data['link_token']
    print(f"✅ Link token created (expires in 4 hours):\n\n{token}\n")
    print("Paste this into the Plaid Link page or use it in the browser flow.")
    return token


def exchange_public_token(public_token):
    """Exchange a public token for a persistent access token."""
    data = plaid_request('/item/public_token/exchange', {
        'public_token': public_token,
    })
    access_token = data['access_token']
    item_id = data['item_id']
    print(f"✅ Access token obtained!")
    print(f"   Item ID: {item_id}")
    print(f"   Access Token: {access_token}")
    print(f"\n⚠️  Store this as PLAID_ACCESS_TOKEN env var. Do NOT commit to git.")
    return access_token


def get_balances():
    """Get current account balances."""
    if not PLAID_ACCESS_TOKEN:
        print("❌ PLAID_ACCESS_TOKEN not set. Run 'exchange' first.", file=sys.stderr)
        sys.exit(1)
    
    data = plaid_request('/accounts/balance/get', {
        'access_token': PLAID_ACCESS_TOKEN,
    })
    
    print("🏦 Account Balances\n")
    for acct in data.get('accounts', []):
        name = acct.get('name', 'Unknown')
        bal = acct.get('balances', {})
        current = bal.get('current', 0)
        available = bal.get('available', 'N/A')
        acct_type = acct.get('subtype', acct.get('type', ''))
        mask = acct.get('mask', '****')
        print(f"  {name} (****{mask}) [{acct_type}]")
        print(f"    Current:   ${current:,.2f}")
        print(f"    Available: ${available:,.2f}" if isinstance(available, (int, float)) else f"    Available: {available}")
        print()
    
    return data['accounts']


def get_transactions(days=30):
    """Get recent transactions."""
    if not PLAID_ACCESS_TOKEN:
        print("❌ PLAID_ACCESS_TOKEN not set. Run 'exchange' first.", file=sys.stderr)
        sys.exit(1)
    
    end_date = datetime.now().strftime('%Y-%m-%d')
    start_date = (datetime.now() - timedelta(days=days)).strftime('%Y-%m-%d')
    
    data = plaid_request('/transactions/get', {
        'access_token': PLAID_ACCESS_TOKEN,
        'start_date': start_date,
        'end_date': end_date,
        'options': {'count': 100, 'offset': 0},
    })
    
    transactions = data.get('transactions', [])
    total_count = data.get('total_transactions', 0)
    
    print(f"💳 Transactions (last {days} days) — {total_count} total\n")
    
    # Categorize
    categories = {}
    total_in = 0
    total_out = 0
    
    for tx in transactions:
        name = tx.get('name', 'Unknown')
        amount = tx.get('amount', 0)  # Plaid: positive = money out, negative = money in
        date = tx.get('date', '')
        cat = tx.get('personal_finance_category', {}).get('primary', 'UNCATEGORIZED')
        
        if amount > 0:
            total_out += amount
        else:
            total_in += abs(amount)
        
        categories.setdefault(cat, {'count': 0, 'total': 0})
        categories[cat]['count'] += 1
        categories[cat]['total'] += amount
        
        print(f"  {date}  {'🔴' if amount > 0 else '🟢'} ${abs(amount):>10,.2f}  {name[:40]:<40}  [{cat}]")
    
    print(f"\n{'='*70}")
    print(f"  Total In:  🟢 ${total_in:,.2f}")
    print(f"  Total Out: 🔴 ${total_out:,.2f}")
    print(f"  Net:       {'🟢' if total_in > total_out else '🔴'} ${abs(total_in - total_out):,.2f}")
    print(f"\n📊 By Category:")
    for cat, info in sorted(categories.items(), key=lambda x: abs(x[1]['total']), reverse=True):
        print(f"  {cat:<30} {info['count']:>3} txns  ${abs(info['total']):>10,.2f}")
    
    return transactions


def weekly_summary():
    """Generate a weekly financial summary for memory files."""
    accounts = get_balances()
    transactions = get_transactions(days=7)
    
    # Build summary
    total_balance = sum(a.get('balances', {}).get('current', 0) for a in accounts)
    total_in = sum(abs(t['amount']) for t in transactions if t.get('amount', 0) < 0)
    total_out = sum(t['amount'] for t in transactions if t.get('amount', 0) > 0)
    
    summary = f"""# Financial Summary — Week of {datetime.now().strftime('%Y-%m-%d')}

## Balances
"""
    for acct in accounts:
        name = acct.get('name', 'Unknown')
        bal = acct.get('balances', {}).get('current', 0)
        summary += f"- {name}: ${bal:,.2f}\n"
    
    summary += f"\n**Total: ${total_balance:,.2f}**\n"
    summary += f"""
## Weekly Cash Flow
- Money In: ${total_in:,.2f}
- Money Out: ${total_out:,.2f}
- Net: ${abs(total_in - total_out):,.2f} {'surplus' if total_in > total_out else 'deficit'}

## Top Expenses
"""
    expenses = sorted([t for t in transactions if t.get('amount', 0) > 0], key=lambda x: x['amount'], reverse=True)[:5]
    for tx in expenses:
        summary += f"- ${tx['amount']:,.2f} — {tx.get('name', 'Unknown')} ({tx.get('date', '')})\n"
    
    # Save to memory
    mem_dir = Path('/home/node/.openclaw/workspace/memory/financial')
    mem_dir.mkdir(parents=True, exist_ok=True)
    filepath = mem_dir / f"{datetime.now().strftime('%Y-%m-%d')}-weekly.md"
    filepath.write_text(summary)
    print(f"\n📝 Summary saved to {filepath}")
    
    return summary


def sandbox_test():
    """Test with Plaid sandbox — creates a fake link and pulls test data."""
    print("🧪 Running Plaid Sandbox Test...\n")
    
    # Create sandbox public token directly (no Link UI needed)
    data = plaid_request('/sandbox/public_token/create', {
        'institution_id': 'ins_109508',  # First Platypus Bank (sandbox)
        'initial_products': ['transactions'],
    })
    
    public_token = data['public_token']
    print(f"  Sandbox public token: {public_token[:20]}...")
    
    # Exchange it
    exchange_data = plaid_request('/item/public_token/exchange', {
        'public_token': public_token,
    })
    
    access_token = exchange_data['access_token']
    print(f"  Access token obtained: {access_token[:20]}...")
    
    # Get balances
    balance_data = plaid_request('/accounts/balance/get', {
        'access_token': access_token,
    })
    
    print(f"\n🏦 Sandbox Account Balances:\n")
    for acct in balance_data.get('accounts', []):
        name = acct.get('name', 'Unknown')
        bal = acct.get('balances', {})
        current = bal.get('current') or 0
        available = bal.get('available') or 0
        print(f"  {name}: ${current:,.2f} (available: ${available:,.2f})")
    
    # Get transactions
    end_date = datetime.now().strftime('%Y-%m-%d')
    start_date = (datetime.now() - timedelta(days=30)).strftime('%Y-%m-%d')
    
    tx_data = plaid_request('/transactions/get', {
        'access_token': access_token,
        'start_date': start_date,
        'end_date': end_date,
        'options': {'count': 10},
    })
    
    print(f"\n💳 Sample Transactions ({tx_data.get('total_transactions', 0)} total):\n")
    for tx in tx_data.get('transactions', [])[:10]:
        amount = tx.get('amount', 0)
        print(f"  {tx.get('date', '')}  {'🔴' if amount > 0 else '🟢'} ${abs(amount):>8,.2f}  {tx.get('name', '')}")
    
    print(f"\n✅ Sandbox test complete! Plaid integration is working.")
    print(f"   To connect real accounts, switch PLAID_ENV=development and get Development keys.")
    return access_token


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Plaid Financial OS')
    parser.add_argument('command', choices=['create-link-token', 'exchange', 'balances', 'transactions', 'summary', 'sandbox-test'])
    parser.add_argument('public_token', nargs='?', help='Public token for exchange command')
    parser.add_argument('--days', type=int, default=30, help='Days of transactions to fetch')
    args = parser.parse_args()
    
    if args.command == 'create-link-token':
        create_link_token()
    elif args.command == 'exchange':
        if not args.public_token:
            print("❌ Usage: plaid_client.py exchange <public_token>", file=sys.stderr)
            sys.exit(1)
        exchange_public_token(args.public_token)
    elif args.command == 'balances':
        get_balances()
    elif args.command == 'transactions':
        get_transactions(days=args.days)
    elif args.command == 'summary':
        weekly_summary()
    elif args.command == 'sandbox-test':
        sandbox_test()
