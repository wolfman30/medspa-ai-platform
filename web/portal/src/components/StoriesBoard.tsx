import { useEffect, useState, useCallback } from 'react';
import { listStories, createStory, updateStory, deleteStory, getStory, type Story } from '../api/client';

const COLUMNS = [
  { key: 'backlog', label: 'Backlog', color: 'bg-slate-100' },
  { key: 'in_progress', label: 'In Progress', color: 'bg-blue-50' },
  { key: 'review', label: 'Review', color: 'bg-yellow-50' },
  { key: 'done', label: 'Done', color: 'bg-green-50' },
] as const;

const PRIORITY_BADGE: Record<string, { label: string; cls: string }> = {
  critical: { label: 'Critical', cls: 'bg-red-100 text-red-800 border-red-200' },
  high: { label: 'High', cls: 'bg-orange-100 text-orange-800 border-orange-200' },
  medium: { label: 'Medium', cls: 'bg-blue-100 text-blue-800 border-blue-200' },
  low: { label: 'Low', cls: 'bg-slate-100 text-slate-600 border-slate-200' },
};

const NEXT_STATUS: Record<string, string[]> = {
  backlog: ['in_progress'],
  in_progress: ['review', 'backlog'],
  review: ['done', 'in_progress'],
  done: ['backlog'],
};

// ── Card ─────────────────────────────────────────────────────────────

function StoryCard({ story, onClick }: { story: Story; onClick: () => void }) {
  const badge = PRIORITY_BADGE[story.priority] || PRIORITY_BADGE.medium;
  return (
    <button
      onClick={onClick}
      className="w-full text-left ui-card p-3 hover:shadow-md transition-shadow cursor-pointer border border-slate-200 rounded-xl"
    >
      <div className="flex items-start justify-between gap-2">
        <h4 className="text-sm font-medium text-slate-900 leading-snug">{story.title}</h4>
        <span className={`shrink-0 text-[10px] font-semibold px-1.5 py-0.5 rounded border ${badge.cls}`}>
          {badge.label}
        </span>
      </div>
      <div className="mt-2 flex items-center gap-2 flex-wrap">
        {story.subTaskCount > 0 && (
          <span className="text-[11px] text-slate-500">
            📋 {story.subTaskCount} sub-task{story.subTaskCount !== 1 ? 's' : ''}
          </span>
        )}
        {story.labels.map((l) => (
          <span key={l} className="text-[10px] bg-violet-50 text-violet-700 px-1.5 py-0.5 rounded-full border border-violet-200">
            {l}
          </span>
        ))}
      </div>
      {story.assignedTo && (
        <div className="mt-1 text-[11px] text-slate-400">→ {story.assignedTo}</div>
      )}
    </button>
  );
}

// ── Detail View ──────────────────────────────────────────────────────

function StoryDetail({
  storyId,
  onBack,
  onRefresh,
}: {
  storyId: string;
  onBack: () => void;
  onRefresh: () => void;
}) {
  const [story, setStory] = useState<Story | null>(null);
  const [subTasks, setSubTasks] = useState<Story[]>([]);
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState({ title: '', description: '', priority: '', labels: '', assignedTo: '' });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const data = await getStory(storyId);
      setStory(data.story);
      setSubTasks(data.subTasks || []);
    } catch {
      setError('Failed to load story');
    }
  }, [storyId]);

  useEffect(() => { load(); }, [load]);

  const startEdit = () => {
    if (!story) return;
    setForm({
      title: story.title,
      description: story.description,
      priority: story.priority,
      labels: story.labels.join(', '),
      assignedTo: story.assignedTo,
    });
    setEditing(true);
  };

  const handleSave = async () => {
    if (!story) return;
    setSaving(true);
    try {
      await updateStory(story.id, {
        title: form.title,
        description: form.description,
        priority: form.priority,
        labels: form.labels.split(',').map((s) => s.trim()).filter(Boolean),
        assignedTo: form.assignedTo,
      });
      setEditing(false);
      load();
      onRefresh();
    } catch {
      setError('Failed to save');
    } finally {
      setSaving(false);
    }
  };

  const handleMove = async (newStatus: string) => {
    if (!story) return;
    try {
      await updateStory(story.id, { status: newStatus });
      load();
      onRefresh();
    } catch {
      setError('Failed to move story');
    }
  };

  const handleDelete = async () => {
    if (!story || !confirm('Delete this story?')) return;
    try {
      await deleteStory(story.id);
      onRefresh();
      onBack();
    } catch {
      setError('Failed to delete');
    }
  };

  if (!story) {
    return (
      <div className="p-6 text-center text-slate-500">
        {error || 'Loading...'}
      </div>
    );
  }

  const badge = PRIORITY_BADGE[story.priority] || PRIORITY_BADGE.medium;
  const moves = NEXT_STATUS[story.status] || [];

  return (
    <div className="ui-container py-6 max-w-2xl mx-auto">
      <button onClick={onBack} className="ui-btn ui-btn-ghost text-sm mb-4">← Back</button>

      {error && <div className="mb-4 p-3 bg-red-50 text-red-700 rounded-xl text-sm">{error}</div>}

      {editing ? (
        <div className="ui-card p-6 space-y-4 border border-slate-200 rounded-xl">
          <div>
            <label className="ui-label">Title</label>
            <input className="ui-input mt-1" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
          </div>
          <div>
            <label className="ui-label">Description</label>
            <textarea className="ui-input mt-1 min-h-[120px]" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="ui-label">Priority</label>
              <select className="ui-select mt-1" value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}>
                <option value="critical">Critical</option>
                <option value="high">High</option>
                <option value="medium">Medium</option>
                <option value="low">Low</option>
              </select>
            </div>
            <div>
              <label className="ui-label">Assigned To</label>
              <input className="ui-input mt-1" value={form.assignedTo} onChange={(e) => setForm({ ...form, assignedTo: e.target.value })} />
            </div>
          </div>
          <div>
            <label className="ui-label">Labels (comma-separated)</label>
            <input className="ui-input mt-1" value={form.labels} onChange={(e) => setForm({ ...form, labels: e.target.value })} />
          </div>
          <div className="flex gap-2">
            <button onClick={handleSave} disabled={saving} className="ui-btn ui-btn-primary">{saving ? 'Saving...' : 'Save'}</button>
            <button onClick={() => setEditing(false)} className="ui-btn ui-btn-ghost">Cancel</button>
          </div>
        </div>
      ) : (
        <div className="ui-card p-6 border border-slate-200 rounded-xl space-y-4">
          <div className="flex items-start justify-between">
            <h2 className="text-lg font-semibold text-slate-900">{story.title}</h2>
            <span className={`text-xs font-semibold px-2 py-1 rounded border ${badge.cls}`}>{badge.label}</span>
          </div>

          {story.description && (
            <p className="text-sm text-slate-700 whitespace-pre-wrap">{story.description}</p>
          )}

          <div className="flex flex-wrap gap-1">
            {story.labels.map((l) => (
              <span key={l} className="text-xs bg-violet-50 text-violet-700 px-2 py-0.5 rounded-full border border-violet-200">{l}</span>
            ))}
          </div>

          {story.assignedTo && (
            <div className="text-sm text-slate-500">Assigned to: <span className="font-medium text-slate-700">{story.assignedTo}</span></div>
          )}

          <div className="text-xs text-slate-400">
            Status: {story.status.replace('_', ' ')} · Created {new Date(story.createdAt).toLocaleDateString()}
            {story.completedAt && ` · Completed ${new Date(story.completedAt).toLocaleDateString()}`}
          </div>

          <div className="flex flex-wrap gap-2 pt-2 border-t border-slate-100">
            {moves.map((s) => (
              <button key={s} onClick={() => handleMove(s)} className="ui-btn ui-btn-ghost text-sm">
                Move to {s.replace('_', ' ')}
              </button>
            ))}
            <button onClick={startEdit} className="ui-btn ui-btn-ghost text-sm">Edit</button>
            <button onClick={handleDelete} className="ui-btn ui-btn-ghost text-sm text-red-600">Delete</button>
          </div>
        </div>
      )}

      {subTasks.length > 0 && (
        <div className="mt-6">
          <h3 className="text-sm font-semibold text-slate-700 mb-3">Sub-tasks</h3>
          <div className="space-y-2">
            {subTasks.map((st) => {
              const b = PRIORITY_BADGE[st.priority] || PRIORITY_BADGE.medium;
              return (
                <div key={st.id} className="flex items-center justify-between p-3 bg-slate-50 rounded-lg border border-slate-200">
                  <div>
                    <span className="text-sm text-slate-900">{st.title}</span>
                    <span className={`ml-2 text-[10px] font-semibold px-1.5 py-0.5 rounded border ${b.cls}`}>{b.label}</span>
                  </div>
                  <span className="text-xs text-slate-500">{st.status.replace('_', ' ')}</span>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

// ── Create Form ──────────────────────────────────────────────────────

function CreateStoryForm({
  parentStories,
  onCreated,
  onCancel,
}: {
  parentStories: Story[];
  onCreated: () => void;
  onCancel: () => void;
}) {
  const [form, setForm] = useState({
    title: '',
    description: '',
    priority: 'medium',
    labels: '',
    parentId: '',
    assignedTo: '',
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.title.trim()) return;
    setSaving(true);
    setError('');
    try {
      await createStory({
        title: form.title.trim(),
        description: form.description,
        priority: form.priority,
        labels: form.labels.split(',').map((s) => s.trim()).filter(Boolean),
        parentId: form.parentId || undefined,
        assignedTo: form.assignedTo,
      });
      onCreated();
    } catch {
      setError('Failed to create story');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="ui-card p-6 border border-slate-200 rounded-xl space-y-4 max-w-lg mx-auto">
      <h3 className="text-lg font-semibold text-slate-900">New Story</h3>
      {error && <div className="p-3 bg-red-50 text-red-700 rounded-xl text-sm">{error}</div>}
      <div>
        <label className="ui-label">Title *</label>
        <input className="ui-input mt-1" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} required />
      </div>
      <div>
        <label className="ui-label">Description</label>
        <textarea className="ui-input mt-1 min-h-[80px]" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="ui-label">Priority</label>
          <select className="ui-select mt-1" value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>
        <div>
          <label className="ui-label">Assigned To</label>
          <input className="ui-input mt-1" value={form.assignedTo} onChange={(e) => setForm({ ...form, assignedTo: e.target.value })} />
        </div>
      </div>
      <div>
        <label className="ui-label">Labels (comma-separated)</label>
        <input className="ui-input mt-1" value={form.labels} onChange={(e) => setForm({ ...form, labels: e.target.value })} placeholder="frontend, bug, ..." />
      </div>
      <div>
        <label className="ui-label">Parent Story</label>
        <select className="ui-select mt-1" value={form.parentId} onChange={(e) => setForm({ ...form, parentId: e.target.value })}>
          <option value="">None</option>
          {parentStories.map((s) => (
            <option key={s.id} value={s.id}>{s.title}</option>
          ))}
        </select>
      </div>
      <div className="flex gap-2">
        <button type="submit" disabled={saving || !form.title.trim()} className="ui-btn ui-btn-primary">
          {saving ? 'Creating...' : 'Create Story'}
        </button>
        <button type="button" onClick={onCancel} className="ui-btn ui-btn-ghost">Cancel</button>
      </div>
    </form>
  );
}

// ── Board ────────────────────────────────────────────────────────────

export function StoriesBoard() {
  const [stories, setStories] = useState<Story[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [error, setError] = useState('');
  const [filters, setFilters] = useState<{ status: string; priority: string; label: string }>({
    status: '',
    priority: '',
    label: '',
  });
  const [labelInput, setLabelInput] = useState('');

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      setFilters((prev) => ({ ...prev, label: labelInput }));
    }, 300);
    return () => clearTimeout(timeoutId);
  }, [labelInput]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const trimmedLabel = filters.label.trim();
      const data = await listStories({
        status: filters.status || undefined,
        priority: filters.priority || undefined,
        label: trimmedLabel || undefined,
      });
      setStories(data.stories);
      setError('');
    } catch {
      setError('Failed to load stories');
    } finally {
      setLoading(false);
    }
  }, [filters]);

  useEffect(() => { load(); }, [load]);

  if (selectedId) {
    return <StoryDetail storyId={selectedId} onBack={() => setSelectedId(null)} onRefresh={load} />;
  }

  if (showCreate) {
    return (
      <div className="ui-container py-6">
        <CreateStoryForm
          parentStories={stories.filter((s) => !s.parentId)}
          onCreated={() => { setShowCreate(false); load(); }}
          onCancel={() => setShowCreate(false)}
        />
      </div>
    );
  }

  return (
    <div className="ui-container py-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-slate-900">Stories</h2>
        <button onClick={() => setShowCreate(true)} className="ui-btn ui-btn-primary text-sm">
          + Add Story
        </button>
      </div>

      <div className="mb-6 grid grid-cols-1 sm:grid-cols-4 gap-2">
        <select
          className="ui-select"
          value={filters.status}
          onChange={(e) => setFilters((prev) => ({ ...prev, status: e.target.value }))}
        >
          <option value="">All statuses</option>
          <option value="backlog">Backlog</option>
          <option value="in_progress">In Progress</option>
          <option value="review">Review</option>
          <option value="done">Done</option>
        </select>
        <select
          className="ui-select"
          value={filters.priority}
          onChange={(e) => setFilters((prev) => ({ ...prev, priority: e.target.value }))}
        >
          <option value="">All priorities</option>
          <option value="critical">Critical</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
        <input
          className="ui-input"
          placeholder="Filter by label..."
          value={labelInput}
          onChange={(e) => setLabelInput(e.target.value)}
        />
        <button
          className="ui-btn ui-btn-ghost"
          onClick={() => {
            setLabelInput('');
            setFilters({ status: '', priority: '', label: '' });
          }}
        >
          Clear filters
        </button>
      </div>

      {error && <div className="mb-4 p-3 bg-red-50 text-red-700 rounded-xl text-sm">{error}</div>}

      {loading ? (
        <div className="flex justify-center py-12">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {COLUMNS.map((col) => {
            const colStories = stories.filter((s) => s.status === col.key);
            return (
              <div key={col.key} className={`rounded-xl p-3 ${col.color} min-h-[200px]`}>
                <div className="flex items-center justify-between mb-3">
                  <h3 className="text-sm font-semibold text-slate-700">{col.label}</h3>
                  <span className="text-xs text-slate-500 bg-white px-2 py-0.5 rounded-full">{colStories.length}</span>
                </div>
                <div className="space-y-2">
                  {colStories.map((s) => (
                    <StoryCard key={s.id} story={s} onClick={() => setSelectedId(s.id)} />
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
