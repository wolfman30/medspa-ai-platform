import { useCallback, useEffect, useMemo, useState } from 'react';
import { getBalances, getBudget, getTransactions, updateBudget, type FinanceAccount, type FinanceBudgetResponse, type FinanceTransaction } from '../api/client';

const CATEGORY_ICONS: Record<string, string> = {
  FOOD_AND_DRINK: '🍽️',
  TRANSPORTATION: '🚗',
  GENERAL_SERVICES: '🧾',
  ENTERTAINMENT: '🎬',
  GENERAL_MERCHANDISE: '🛍️',
  PERSONAL_CARE: '🧴',
  LOAN_PAYMENTS: '💳',
  RENT_AND_UTILITIES: '🏠',
  UNCATEGORIZED: '📦',
};

function money(v: number): string {
  return `$${v.toLocaleString(undefined, { maximumFractionDigits: 2 })}`;
}

export function FinanceDashboard() {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [budget, setBudget] = useState<FinanceBudgetResponse | null>(null);
  const [balances, setBalances] = useState<FinanceAccount[]>([]);
  const [transactions, setTransactions] = useState<FinanceTransaction[]>([]);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, number>>({});

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      const [budgetData, balanceData, txData] = await Promise.all([
        getBudget(),
        getBalances(),
        getTransactions(30),
      ]);
      setBudget(budgetData);
      setBalances(balanceData.accounts || []);
      setTransactions(txData.transactions || []);
      setDraft(Object.fromEntries(Object.entries(budgetData.categories).map(([k, v]) => [k, v.allocated])));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load finance data');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const txByCategory = useMemo(() => {
    const grouped: Record<string, FinanceTransaction[]> = {};
    for (const tx of transactions) {
      const key = tx.personal_finance_category?.primary || 'UNCATEGORIZED';
      if (!grouped[key]) grouped[key] = [];
      grouped[key].push(tx);
    }
    return grouped;
  }, [transactions]);

  const checking = balances.find(a => a.subtype?.toLowerCase().includes('checking'));
  const savings = balances.find(a => a.subtype?.toLowerCase().includes('savings'));

  const saveBudget = async () => {
    if (!budget) return;
    const categories = Object.fromEntries(
      Object.entries(budget.categories).map(([key, value]) => [
        key,
        { label: value.label, allocated: Number(draft[key] ?? value.allocated) },
      ])
    );
    await updateBudget(categories, budget.month);
    setEditing(false);
    await refresh();
  };

  if (loading) {
    return <div className="ui-page p-8"><div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" /></div>;
  }

  if (error || !budget) {
    return <div className="ui-page p-8"><div className="ui-card p-4 text-red-700">{error || 'No data'}</div></div>;
  }

  return (
    <div className="ui-page p-4 sm:p-8">
      <div className="max-w-7xl mx-auto space-y-5">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-semibold text-slate-900">Finance Dashboard</h1>
          <div className="flex gap-2">
            <button className="ui-btn ui-btn-ghost" onClick={refresh}>↻ Refresh</button>
            {!editing ? (
              <button className="ui-btn ui-btn-primary" onClick={() => setEditing(true)}>Edit Budget</button>
            ) : (
              <>
                <button className="ui-btn ui-btn-ghost" onClick={() => setEditing(false)}>Cancel</button>
                <button className="ui-btn ui-btn-primary" onClick={saveBudget}>Save</button>
              </>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="ui-card p-4"><div className="ui-muted text-sm">Total Allocated</div><div className="text-2xl font-bold">{money(budget.totals.allocated)}</div></div>
          <div className="ui-card p-4"><div className="ui-muted text-sm">Total Spent</div><div className="text-2xl font-bold">{money(budget.totals.spent)}</div></div>
          <div className="ui-card p-4"><div className="ui-muted text-sm">Total Remaining</div><div className={`text-2xl font-bold ${budget.totals.remaining >= 0 ? 'text-green-600' : 'text-red-600'}`}>{money(budget.totals.remaining)}</div></div>
        </div>

        <div className="grid lg:grid-cols-3 gap-4">
          <div className="lg:col-span-2 space-y-3">
            {Object.entries(budget.categories).map(([key, cat]) => {
              const pct = cat.allocated > 0 ? Math.min((cat.spent / cat.allocated) * 100, 100) : 0;
              const barColor = pct > 90 ? 'bg-red-500' : pct > 75 ? 'bg-orange-500' : 'bg-green-500';
              const items = txByCategory[key] || [];

              return (
                <div key={key} className="ui-card p-4">
                  <button className="w-full text-left" onClick={() => setExpanded(prev => ({ ...prev, [key]: !prev[key] }))}>
                    <div className="flex justify-between items-center gap-3">
                      <div className="font-medium text-slate-900">{CATEGORY_ICONS[key] || '📁'} {cat.label}</div>
                      <div className="text-sm ui-muted">{money(cat.spent)} spent of {money(cat.allocated)}</div>
                    </div>
                    <div className="mt-2 h-3 rounded bg-slate-200 overflow-hidden">
                      <div className={`h-full ${barColor}`} style={{ width: `${pct}%` }} />
                    </div>
                    <div className={`text-sm mt-2 ${cat.remaining >= 0 ? 'text-green-700' : 'text-red-700'}`}>{money(cat.remaining)} remaining</div>
                  </button>

                  {editing && (
                    <div className="mt-3">
                      <label className="ui-label">Allocated</label>
                      <input
                        type="number"
                        className="ui-input mt-1"
                        value={draft[key] ?? cat.allocated}
                        onChange={e => setDraft(prev => ({ ...prev, [key]: Number(e.target.value) }))}
                      />
                    </div>
                  )}

                  {expanded[key] && (
                    <div className="mt-3 border-t border-slate-200 pt-3 space-y-2">
                      {items.length === 0 ? (
                        <div className="text-sm ui-muted">No transactions in this category.</div>
                      ) : items.map(tx => (
                        <div key={tx.transaction_id} className="flex justify-between text-sm">
                          <div>
                            <div className="text-slate-800">{tx.name}</div>
                            <div className="ui-muted">{tx.date}</div>
                          </div>
                          <div className="font-medium">{money(tx.amount)}</div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          <div className="ui-card p-4 h-fit">
            <h2 className="text-lg font-semibold text-slate-900 mb-3">Account Balances</h2>
            <div className="space-y-3">
              <div>
                <div className="ui-muted text-sm">Checking</div>
                <div className="text-xl font-semibold">{money(checking?.balances.available ?? checking?.balances.current ?? 0)}</div>
              </div>
              <div>
                <div className="ui-muted text-sm">Savings</div>
                <div className="text-xl font-semibold">{money(savings?.balances.available ?? savings?.balances.current ?? 0)}</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
