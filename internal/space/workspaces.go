package space

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type workspaceRecord struct {
	Path       string   `json:"path"`
	Title      string   `json:"title"`
	SessionIDs []string `json:"sessionIds"`
}
type workspaceDocument struct {
	Unit struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	} `json:"unit"`
	Global struct {
		Initialized  *bool           `json:"initialized"`
		WorkspaceIDs []string        `json:"workspaceIds"`
		Archived     []string        `json:"archivedSessionIds"`
		Pinned       []string        `json:"pinnedSessionIds"`
		Pending      json.RawMessage `json:"pendingMutation"`
	} `json:"global"`
	Tables struct {
		Workspaces map[string]workspaceRecord `json:"workspaces"`
	} `json:"tables"`
}

func workspaceKey(id string) string { return conversationKey("workspace:" + id) }
func pathKey(path string) string {
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(path))
}
func workspaceName(path string) string {
	if path == "" {
		return "未记录工作区"
	}
	name := filepath.Base(filepath.Clean(path))
	if name == "default-workspace" {
		return "默认工作区"
	}
	return name
}
func (s *Service) workspaceDocument() (workspaceDocument, []byte, error) {
	var doc workspaceDocument
	path := filepath.Join(s.Settings().Source, "storages", "workspace.json")
	info, e := regular(path)
	if os.IsNotExist(e) {
		return doc, nil, nil
	}
	if e != nil {
		return doc, nil, e
	}
	if info.Size() > 8<<20 {
		return doc, nil, fmt.Errorf("工作区登记文件过大")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return doc, nil, e
	}
	if json.Unmarshal(data, &doc) != nil || doc.Unit.Name != "workspace" || doc.Unit.Version != 2 || doc.Tables.Workspaces == nil || doc.Global.Initialized == nil {
		return doc, nil, fmt.Errorf("工作区登记格式未识别")
	}
	if len(doc.Global.Pending) > 0 && string(doc.Global.Pending) != "null" {
		return doc, nil, fmt.Errorf("Harness 工作区变更尚未完成，请先由 Harness 完成恢复")
	}
	seen := map[string]bool{}
	paths := map[string]bool{}
	for _, id := range doc.Global.WorkspaceIDs {
		r, ok := doc.Tables.Workspaces[id]
		if !ok || id == "" || seen[id] || r.Path == "" || r.Title == "" || paths[pathKey(r.Path)] {
			return doc, nil, fmt.Errorf("工作区登记缺失或重复")
		}
		seen[id] = true
		paths[pathKey(r.Path)] = true
	}
	if len(seen) != len(doc.Tables.Workspaces) {
		return doc, nil, fmt.Errorf("工作区登记顺序与条目不一致")
	}
	return doc, data, nil
}

// 按 Harness 的工作区登记顺序、名称和路径分组；未登记的会话仍按 cwd 完整呈现。
func (s *Service) attachWorkspaces(catalog *Catalog) {
	catalog.Workspaces = []Workspace{}
	doc, _, e := s.workspaceDocument()
	if e != nil {
		catalog.Warnings = append(catalog.Warnings, e.Error())
	}
	byPath := map[string]string{}
	if e == nil {
		for _, id := range doc.Global.WorkspaceIDs {
			record := doc.Tables.Workspaces[id]
			title := record.Title
			if title == "default-workspace" {
				title = "默认工作区"
			}
			w := Workspace{Key: workspaceKey(id), ID: id, Title: title, Path: record.Path, Registered: true}
			catalog.Workspaces = append(catalog.Workspaces, w)
			byPath[pathKey(record.Path)] = w.Key
		}
	}
	archived, pinned := map[string]bool{}, map[string]bool{}
	for _, id := range doc.Global.Archived {
		archived[id] = true
	}
	for _, id := range doc.Global.Pinned {
		pinned[id] = true
	}
	for i := range catalog.Items {
		c := &catalog.Items[i]
		key, ok := byPath[pathKey(c.CWD)]
		if !ok {
			key = workspaceKey("cwd:" + pathKey(c.CWD))
			byPath[pathKey(c.CWD)] = key
			catalog.Workspaces = append(catalog.Workspaces, Workspace{Key: key, Title: workspaceName(c.CWD), Path: c.CWD})
		}
		c.WorkspaceKey = key
		c.Archived = archived[c.ID]
		c.Pinned = pinned[c.ID]
	}
}
