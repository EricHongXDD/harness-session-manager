package space

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type DeleteRequest struct {
	Kind         string   `json:"kind"`
	Keys         []string `json:"keys,omitempty"`
	WorkspaceKey string   `json:"workspaceKey,omitempty"`
	HiddenID     string   `json:"hiddenId,omitempty"`
}
type DeleteItem struct {
	Title  string `json:"title"`
	CWD    string `json:"cwd"`
	Hidden bool   `json:"hidden"`
}
type DeletePreview struct {
	Token          string       `json:"token"`
	Items          []DeleteItem `json:"items"`
	Images         int          `json:"images"`
	WorkspaceTitle string       `json:"workspaceTitle,omitempty"`
	Source         string       `json:"source"`
}
type deleteTarget struct {
	Root     string
	Member   Member
	HiddenID string
	CWD      string
}
type deletePlan struct {
	Request     DeleteRequest
	At          time.Time
	Fingerprint string
	Targets     []deleteTarget
	Images      []File
	Registry    []byte
	WorkspaceID string
	Preview     DeletePreview
}
type DeleteResult struct {
	Conversations int `json:"conversations"`
	Images        int `json:"images"`
}

// 永久删除只接受一次有效的范围预览；确认前来源、成员或附件引用变化均使预览失效。
func (s *Service) PreviewDelete(request DeleteRequest) (DeletePreview, error) {
	if e := s.lock(); e != nil {
		return DeletePreview{}, e
	}
	defer s.operation.Unlock()
	plan, e := s.planDelete(request)
	if e != nil {
		return DeletePreview{}, e
	}
	token := newID()
	plan.Preview.Token = token
	plan.At = time.Now()
	if len(s.deletePlans) >= 16 {
		s.deletePlans = nil
	}
	if s.deletePlans == nil {
		s.deletePlans = map[string]deletePlan{}
	}
	s.deletePlans[token] = plan
	return plan.Preview, nil
}

func (s *Service) planDelete(request DeleteRequest) (deletePlan, error) {
	plan := deletePlan{Request: request, Targets: []deleteTarget{}, Images: []File{}, Preview: DeletePreview{Items: []DeleteItem{}}}
	if e := s.guard(); e != nil {
		return plan, e
	}
	cfg := s.Settings()
	plan.Preview.Source = cfg.Source
	catalog, e := s.List()
	if e != nil {
		return plan, e
	}
	if len(catalog.Warnings) > 0 {
		return plan, fmt.Errorf("来源未完整识别，不能确认永久删除范围：%s", catalog.Warnings[0])
	}
	root := filepath.Join(cfg.Source, "sessions")
	identity, _, e := nativeStat(root)
	if e != nil {
		return plan, e
	}
	entries := []Conversation{}
	roots := []string{}
	hiddenIDs := []string{}
	for _, c := range catalog.Items {
		entries = append(entries, c)
		roots = append(roots, root)
		hiddenIDs = append(hiddenIDs, "")
	}
	hidden, e := s.Hidden()
	if e != nil {
		return plan, e
	}
	for _, record := range hidden {
		if record.Source != cfg.Source {
			continue
		}
		if record.SourceIdentity != identity {
			return plan, fmt.Errorf("隐藏记录的来源身份已变化，不能确认图片共享关系")
		}
		hiddenRoot := filepath.Join(filepath.Dir(record.Path), "sessions")
		for _, relative := range record.Moved {
			if !safeRelative(relative) || len(strings.Split(relative, "/")) != 2 {
				return plan, fmt.Errorf("隐藏路径无效")
			}
			log, version, e := selectedLog(filepath.Join(hiddenRoot, filepath.FromSlash(relative)))
			if e != nil {
				return plan, e
			}
			h, e := readHeader(log, version)
			if e != nil {
				return plan, e
			}
			if relative != filepath.ToSlash(filepath.Join(ProjectDirectory(h.CWD), encodedID(h.ID))) {
				return plan, fmt.Errorf("隐藏目录与身份不符")
			}
			title := s.conversationTitle(h, log, h.ID)
			entries = append(entries, Conversation{ID: h.ID, Title: title, CWD: h.CWD, Parent: h.Parent, Relative: relative, Log: log, Version: version})
			roots = append(roots, hiddenRoot)
			hiddenIDs = append(hiddenIDs, record.ID)
		}
	}
	selected := map[int]bool{}
	ids := map[string]int{}
	for i, c := range entries {
		if c.Warning != "" || c.ID == "" {
			return plan, fmt.Errorf("来源存在未识别会话，不能确认删除与共享图片范围")
		}
		if _, ok := ids[c.ID]; ok {
			return plan, fmt.Errorf("会话 ID 重复，不能删除")
		}
		ids[c.ID] = i
	}
	switch request.Kind {
	case "conversations":
		if len(request.Keys) == 0 || len(request.Keys) > 100 {
			return plan, fmt.Errorf("请选择 1 至 100 个对话")
		}
		for _, key := range request.Keys {
			found := false
			for i, c := range entries {
				if hiddenIDs[i] == "" && c.Key == key {
					selected[i] = true
					found = true
				}
			}
			if !found {
				return plan, fmt.Errorf("部分对话已变化，请刷新")
			}
		}
	case "workspace":
		var workspace *Workspace
		for i := range catalog.Workspaces {
			if catalog.Workspaces[i].Key == request.WorkspaceKey {
				workspace = &catalog.Workspaces[i]
				break
			}
		}
		if workspace == nil {
			return plan, fmt.Errorf("工作区已变化，请刷新")
		}
		plan.Preview.WorkspaceTitle = workspace.Title
		plan.WorkspaceID = workspace.ID
		for i, c := range entries {
			if pathKey(c.CWD) == pathKey(workspace.Path) {
				selected[i] = true
			}
		}
	case "hidden":
		if !validRecordID(request.HiddenID) {
			return plan, fmt.Errorf("隐藏记录无效")
		}
		for i := range entries {
			if hiddenIDs[i] == request.HiddenID {
				selected[i] = true
			}
		}
		if len(selected) == 0 {
			return plan, fmt.Errorf("隐藏会话已变化，请刷新")
		}
	default:
		return plan, fmt.Errorf("删除类型无效")
	}
	// 父子连通组完整纳入确认清单，避免遗留无法独立打开的子会话。
	changed := true
	for changed {
		changed = false
		for i, c := range entries {
			if c.Parent == "" {
				continue
			}
			parent, ok := ids[c.Parent]
			if !ok {
				continue
			}
			if selected[i] && !selected[parent] {
				selected[parent] = true
				changed = true
			}
			if selected[parent] && !selected[i] {
				selected[i] = true
				changed = true
			}
		}
	}
	retained := map[string]bool{}
	candidate := map[string]bool{}
	for i, c := range entries {
		refs, e := logImages(c.Log)
		if e != nil {
			return plan, e
		}
		for _, ref := range refs {
			if selected[i] {
				candidate[ref.ID] = true
			} else {
				retained[ref.ID] = true
			}
		}
		if selected[i] {
			member, e := snapshotMember(roots[i], c)
			if e != nil {
				return plan, e
			}
			plan.Targets = append(plan.Targets, deleteTarget{Root: roots[i], Member: member, HiddenID: hiddenIDs[i], CWD: c.CWD})
			plan.Preview.Items = append(plan.Preview.Items, DeleteItem{Title: c.Title, CWD: c.CWD, Hidden: hiddenIDs[i] != ""})
		}
	}
	for id := range candidate {
		if retained[id] {
			continue
		}
		relative, _ := imagePath(id)
		path := filepath.Join(cfg.Source, filepath.FromSlash(relative))
		file, e := digestFile(path, nil)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return plan, e
		}
		file.Path = relative
		if !validAttachmentFile(file) {
			return plan, fmt.Errorf("图片摘要异常，未执行删除")
		}
		plan.Images = append(plan.Images, file)
	}
	sort.Slice(plan.Images, func(i, j int) bool { return plan.Images[i].Path < plan.Images[j].Path })
	_, plan.Registry, e = s.workspaceDocument()
	if e != nil {
		return plan, e
	}
	// 工作区名称、会话顺序、归档和置顶登记也纳入快照，写回时保留所有其他字段。
	fingerprint, e := json.Marshal(struct {
		Source, Identity string
		Targets          []deleteTarget
		Images           []File
		Registry         []byte
		Workspace        string
	}{cfg.Source, identity, plan.Targets, plan.Images, plan.Registry, plan.WorkspaceID})
	if e != nil {
		return plan, e
	}
	sum := sha256.Sum256(fingerprint)
	plan.Fingerprint = hex.EncodeToString(sum[:])
	plan.Preview.Images = len(plan.Images)
	return plan, nil
}

func (s *Service) DeleteConfirmed(token string) (result DeleteResult, err error) {
	defer func() {
		if err != nil && (result.Conversations > 0 || result.Images > 0) {
			err = fmt.Errorf("已永久删除 %d 个对话和 %d 张图片，剩余步骤未完成：%w", result.Conversations, result.Images, err)
		}
	}()
	if e := s.lock(); e != nil {
		return result, e
	}
	defer s.operation.Unlock()
	plan, ok := s.deletePlans[token]
	delete(s.deletePlans, token)
	if !ok || time.Since(plan.At) > 5*time.Minute {
		return result, fmt.Errorf("删除确认已过期，请重新预览")
	}
	actual, e := s.planDelete(plan.Request)
	if e != nil {
		return result, e
	}
	if actual.Fingerprint != plan.Fingerprint {
		return result, fmt.Errorf("删除范围或来源已变化，未执行删除，请重新确认")
	}
	root := filepath.Join(s.Settings().Source, "sessions")
	affectedHidden := map[string]bool{}
	removedIDs := map[string]bool{}
	for _, target := range actual.Targets {
		if e = s.guard(); e != nil {
			return result, e
		}
		path := filepath.Join(target.Root, filepath.FromSlash(target.Member.Relative))
		if path == target.Root || !within(target.Root, path) || !safeRelative(target.Member.Relative) {
			return result, fmt.Errorf("删除目录越界")
		}
		if e = verifyMember(target.Root, target.Member); e != nil {
			return result, e
		}
		// 明确解析后的目标必须是已核对的两级会话目录，绝不删除项目代码文件夹。
		if e = os.RemoveAll(path); e != nil {
			return result, fmt.Errorf("已删除 %d 个对话，剩余删除失败：%w", result.Conversations, e)
		}
		result.Conversations++
		removedIDs[target.Member.ID] = true
		if target.HiddenID != "" {
			affectedHidden[target.HiddenID] = true
		}
	}
	for _, file := range actual.Images {
		if e = s.guard(); e != nil {
			return result, e
		}
		path := filepath.Join(s.Settings().Source, filepath.FromSlash(file.Path))
		if !validAttachmentFile(file) {
			return result, fmt.Errorf("图片删除路径无效")
		}
		current, e := digestFile(path, nil)
		if e != nil {
			return result, e
		}
		if current.Size != file.Size || current.SHA256 != file.SHA256 {
			return result, fmt.Errorf("图片已变化，未删除图片")
		}
		if e = os.Remove(path); e != nil {
			return result, e
		}
		result.Images++
	}
	if len(actual.Registry) > 0 {
		_, current, e := s.workspaceDocument()
		if e != nil {
			return result, e
		}
		if !reflect.DeepEqual(current, actual.Registry) {
			return result, fmt.Errorf("对话已删除，工作区登记有变化，未覆盖登记")
		}
		data, e := pruneWorkspaceRegistry(actual.Registry, removedIDs, actual.WorkspaceID)
		if e != nil {
			return result, e
		}
		if e = s.guard(); e != nil {
			return result, e
		}
		if e = atomicWrite(filepath.Join(s.Settings().Source, "storages", "workspace.json"), data); e != nil {
			return result, fmt.Errorf("对话已删除，工作区登记清理失败：%w", e)
		}
	}
	for id := range affectedHidden {
		records, e := s.Hidden()
		if e != nil {
			return result, e
		}
		for _, record := range records {
			if record.ID != id {
				continue
			}
			members := []Member{}
			for _, member := range record.Members {
				if !removedIDs[member.ID] {
					members = append(members, member)
				}
			}
			record.Members = members
			if len(members) == 0 {
				if e = s.cleanupDeletedHidden(record); e != nil {
					return result, e
				}
			} else {
				if e = saveRecord(record); e != nil {
					return result, e
				}
			}
		}
	}
	// 只移除已空的会话索引项目目录；登记工作区的实际路径从未用作删除目标。
	for _, target := range actual.Targets {
		if target.Root == root {
			_ = os.Remove(filepath.Dir(filepath.Join(root, filepath.FromSlash(target.Member.Relative))))
		}
	}
	return result, nil
}

func pruneWorkspaceRegistry(data []byte, removed map[string]bool, workspaceID string) ([]byte, error) {
	var doc map[string]json.RawMessage
	if e := json.Unmarshal(data, &doc); e != nil {
		return nil, e
	}
	var global map[string]json.RawMessage
	var tables map[string]json.RawMessage
	var workspaces map[string]json.RawMessage
	if json.Unmarshal(doc["global"], &global) != nil || json.Unmarshal(doc["tables"], &tables) != nil || json.Unmarshal(tables["workspaces"], &workspaces) != nil {
		return nil, fmt.Errorf("工作区登记无法安全修改")
	}
	filter := func(raw json.RawMessage, drop map[string]bool) json.RawMessage {
		var ids []string
		json.Unmarshal(raw, &ids)
		kept := []string{}
		for _, id := range ids {
			if !drop[id] {
				kept = append(kept, id)
			}
		}
		b, _ := json.Marshal(kept)
		return b
	}
	global["archivedSessionIds"] = filter(global["archivedSessionIds"], removed)
	global["pinnedSessionIds"] = filter(global["pinnedSessionIds"], removed)
	if workspaceID != "" {
		delete(workspaces, workspaceID)
		global["workspaceIds"] = filter(global["workspaceIds"], map[string]bool{workspaceID: true})
	}
	for id, raw := range workspaces {
		var record map[string]json.RawMessage
		if e := json.Unmarshal(raw, &record); e != nil {
			return nil, e
		}
		var sessionIDs []string
		if e := json.Unmarshal(record["sessionIds"], &sessionIDs); e != nil {
			return nil, e
		}
		changed := false
		for _, sessionID := range sessionIDs {
			if removed[sessionID] {
				changed = true
				break
			}
		}
		if changed {
			record["sessionIds"] = filter(record["sessionIds"], removed)
			record["updatedAt"], _ = json.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
			workspaces[id], _ = json.Marshal(record)
		}
	}
	tables["workspaces"], _ = json.Marshal(workspaces)
	doc["tables"], _ = json.Marshal(tables)
	doc["global"], _ = json.Marshal(global)
	return json.MarshalIndent(doc, "", "  ")
}

func (s *Service) cleanupDeletedHidden(record Record) error {
	// 永久删除只清理空容器和记录，不调用旧自动 ZIP 清理，手动备份始终独立保留。
	base := filepath.Dir(record.Path)
	root := filepath.Join(s.Settings().Storage, "hidden")
	if base == root || !within(root, base) || filepath.Base(base) != record.ID {
		return fmt.Errorf("隐藏记录删除路径无效")
	}
	if e := noLinks(base); e != nil {
		return e
	}
	sessions := filepath.Join(base, "sessions")
	entries, e := os.ReadDir(sessions)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, entry := range entries {
		if e = os.Remove(filepath.Join(sessions, entry.Name())); e != nil {
			return e
		}
	}
	if e = os.Remove(sessions); e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = os.Remove(record.Path); e != nil {
		return e
	}
	return os.Remove(base)
}
