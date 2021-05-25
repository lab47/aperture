package data

type LockFileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type LockFile []*LockFileEntry
