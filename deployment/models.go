package main

// SavedVPS represents the VPS configuration data structure.
// This matches the schema expected by our Data Proxy service.
type SavedVPS struct {
	ID          string `json:"id,omitempty" bson:"_id,omitempty"`
	UserID      string `json:"user_id"`
	ProjectName string `json:"project_name"`
	IP          string `json:"ip"`
	SSHUser     string `json:"ssh_user"`
	SSHPass     string `json:"ssh_pass"`
}

type FileRequest struct {
	UserID  string `json:"user_id"`
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	NewPath string `json:"new_path,omitempty"`
}

type FileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time"`
}
