package space

import "time"

type Settings struct {
	Demo    bool   `json:"demo"`
	Source  string `json:"source"`
	Storage string `json:"storage"`
}
type Header struct {
	Type        string `json:"type"`
	Version     int    `json:"version"`
	ID          string `json:"id"`
	CWD         string `json:"cwd,omitempty"`
	Created     int64  `json:"createdAt"`
	Parent      string `json:"parentSession,omitempty"`
	Seeded      bool   `json:"isSeeded"`
	Origin      string `json:"origin,omitempty"`
	AgentPreset string `json:"agentPreset,omitempty"`
	Depth       int    `json:"delegationDepth"`
}
type Conversation struct {
	WorkspaceKey string    `json:"workspaceKey"`
	Archived     bool      `json:"archived"`
	Pinned       bool      `json:"pinned"`
	Key          string    `json:"key"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CWD          string    `json:"cwd"`
	Parent       string    `json:"parent"`
	Relative     string    `json:"relative"`
	Path         string    `json:"path"`
	Log          string    `json:"log"`
	Version      int       `json:"version"`
	Created      int64     `json:"created"`
	Modified     time.Time `json:"modified"`
	Bytes        int64     `json:"bytes,string"`
	Files        int       `json:"files"`
	Warning      string    `json:"warning"`
}
type Catalog struct {
	Workspaces []Workspace    `json:"workspaces"`
	Items      []Conversation `json:"items"`
	Warnings   []string       `json:"warnings"`
	Source     string         `json:"source"`
}
type Workspace struct {
	Key        string `json:"key"`
	ID         string `json:"id,omitempty"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Registered bool   `json:"registered"`
}
type Message struct {
	Seq    int64      `json:"seq"`
	Type   string     `json:"type"`
	Role   string     `json:"role"`
	Text   string     `json:"text"`
	Images []ImageRef `json:"images,omitempty"`
}
type ImageRef struct {
	ID        string `json:"attachmentId"`
	Name      string `json:"name,omitempty"`
	MediaType string `json:"mediaType"`
}
type MessagePage struct {
	Items   []Message `json:"items"`
	Next    int       `json:"next"`
	HasMore bool      `json:"hasMore"`
	Warning string    `json:"warning"`
}
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Member struct {
	Relative string `json:"relative"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Files    []File `json:"files"`
}
type Record struct {
	FormatVersion  int       `json:"formatVersion,omitempty"`
	Attachments    []File    `json:"attachments,omitempty"`
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	State          string    `json:"state"`
	Source         string    `json:"source"`
	SourceIdentity string    `json:"sourceIdentity"`
	Created        time.Time `json:"created"`
	Members        []Member  `json:"members"`
	Moved          []string  `json:"moved"`
	Error          string    `json:"error,omitempty"`
	Path           string    `json:"path"`
	Bytes          int64     `json:"bytes,string"`
}
type Category struct {
	Name      string `json:"name"`
	Files     int64  `json:"files"`
	Bytes     int64  `json:"bytes,string"`
	Allocated *int64 `json:"allocated,string"`
}
type Usage struct {
	Running    bool       `json:"running"`
	Complete   bool       `json:"complete"`
	Canceled   bool       `json:"canceled"`
	Root       string     `json:"root"`
	Files      int64      `json:"files"`
	Bytes      int64      `json:"bytes,string"`
	Allocated  *int64     `json:"allocated,string"`
	Categories []Category `json:"categories"`
	Errors     int64      `json:"errors"`
	At         time.Time  `json:"at"`
}
