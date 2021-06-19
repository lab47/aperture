package data

import "time"

type RepoEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	URL         string `json:"url"`

	Dependencies []string          `json:"dependencies"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type RepoIndex struct {
	CreatedAt time.Time   `json:"created_at"`
	Entries   []RepoEntry `json:"entries"`
}
