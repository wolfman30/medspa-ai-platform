CREATE TYPE story_status AS ENUM ('backlog', 'in_progress', 'review', 'done');
CREATE TYPE story_priority AS ENUM ('critical', 'high', 'medium', 'low');

CREATE TABLE stories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status story_status NOT NULL DEFAULT 'backlog',
    priority story_priority NOT NULL DEFAULT 'medium',
    labels TEXT[] NOT NULL DEFAULT '{}',
    parent_id UUID REFERENCES stories(id) ON DELETE SET NULL,
    assigned_to TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_stories_status ON stories(status);
CREATE INDEX idx_stories_priority ON stories(priority);
CREATE INDEX idx_stories_parent ON stories(parent_id);
