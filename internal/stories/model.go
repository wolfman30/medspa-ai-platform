package stories

import "time"

type Story struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Status       string     `json:"status"`
	Priority     string     `json:"priority"`
	Labels       []string   `json:"labels"`
	ParentID     *string    `json:"parentId"`
	AssignedTo   string     `json:"assignedTo"`
	SubTaskCount int        `json:"subTaskCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
}

type CreateStoryRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
	ParentID    *string  `json:"parentId"`
	AssignedTo  string   `json:"assignedTo"`
}

type UpdateStoryRequest struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Status      *string  `json:"status"`
	Priority    *string  `json:"priority"`
	Labels      []string `json:"labels"`
	ParentID    *string  `json:"parentId"`
	AssignedTo  *string  `json:"assignedTo"`
}
