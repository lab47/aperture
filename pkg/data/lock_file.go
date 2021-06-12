package data

import "time"

type LockFileEntry struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`

	RequestedVersion string `json:"requested_verison"`
	ResolvedVersion  string `json:"resolved_version"`
}

type LockFile struct {
	CreatedAt time.Time        `json:"created_at"`
	Sources   []*LockFileEntry `json:"sources"`
}
