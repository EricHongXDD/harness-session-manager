package space

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registerSynthetic(t *testing.T, s *Service, workspaces map[string]any, order []string) {
	t.Helper()
	doc := map[string]any{"unit": map[string]any{"name": "workspace", "version": 2}, "global": map[string]any{"initialized": true, "workspaceIds": order, "defaultWorkspaceId": order[0], "archivedSessionIds": []string{"session-hidden"}, "pinnedSessionIds": []string{"session-live"}, "extra": "原样保留"}, "tables": map[string]any{"workspaces": workspaces}, "extra": map[string]any{"custom": "保留"}}
	data, _ := json.Marshal(doc)
	path := filepath.Join(s.Settings().Source, "storages", "workspace.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func workspaceFixture(path, title string, ids ...string) map[string]any {
	return map[string]any{"path": path, "title": title, "sessionIds": ids, "createdAt": "2026-10-16T00:00:00Z", "updatedAt": "2026-10-16T00:00:00Z"}
}
func rehomeSynthetic(t *testing.T, s *Service, id, cwd string) string {
	t.Helper()
	dir := session(t, s, id, "", false)
	path := filepath.Join(dir, "session.v4.jsonl")
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	split := bytes.IndexByte(data, '\n')
	var header Header
	json.Unmarshal(data[:split], &header)
	header.CWD = cwd
	line, _ := json.Marshal(header)
	if e = os.WriteFile(path, append(append(line, '\n'), data[split+1:]...), 0600); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(s.Settings().Source, "sessions", ProjectDirectory(cwd), id)
	os.MkdirAll(filepath.Dir(target), 0700)
	if e = moveNoReplace(dir, target); e != nil {
		t.Fatal(e)
	}
	return target
}
func keyFor(t *testing.T, s *Service, id string) string {
	t.Helper()
	for _, c := range catalog(t, s).Items {
		if c.ID == id {
			return c.Key
		}
	}
	t.Fatal("未找到合成会话", id)
	return ""
}

func TestHideWithoutZIPAndRestoreRemovesContainer(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-hide", "", false)
	ref, _, path := syntheticImage(t, s)
	attachSynthetic(t, dir, ref)
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	record, e := s.Hide([]string{keyFor(t, s, "session-hide")})
	if e != nil {
		t.Fatal("缺失图片不应妨碍移动会话", e)
	}
	backups, e := s.Backups()
	if e != nil || len(backups) != 0 {
		t.Fatal("隐藏生成了 ZIP", backups, e)
	}
	if len(catalog(t, s).Items) != 0 {
		t.Fatal("隐藏仍留在来源")
	}
	if e = s.RestoreHidden(record.ID); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Hidden()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	if _, e = os.Stat(filepath.Dir(record.Path)); !os.IsNotExist(e) {
		t.Fatal("恢复后隐藏容器仍存在", e)
	}
	backups, e = s.Backups()
	if e != nil || len(backups) != 0 {
		t.Fatal("恢复生成了 ZIP", e)
	}
}

func TestRestoreRemovesLegacyAutomaticZIPButKeepsManualBackup(t *testing.T) {
	s := fixture(t)
	session(t, s, "session-legacy-hidden", "", false)
	key := keyFor(t, s, "session-legacy-hidden")
	manual, e := s.Backup([]string{key})
	if e != nil {
		t.Fatal(e)
	}
	hidden, e := s.Hide([]string{key})
	if e != nil {
		t.Fatal(e)
	}
	// 模拟旧版同一次隐藏创建的独立 ZIP，ID、来源、成员和时间与隐藏记录一致。
	legacy := hidden
	legacy.Kind = "backup"
	legacy.State = "ready"
	path := filepath.Join(s.Settings().Storage, "backups", "legacy-auto.zip")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	for _, member := range hidden.Members {
		for _, file := range member.Files {
			data, e := os.ReadFile(filepath.Join(filepath.Dir(hidden.Path), "sessions", filepath.FromSlash(member.Relative), filepath.FromSlash(file.Path)))
			if e != nil {
				t.Fatal(e)
			}
			w, _ := z.Create("sessions/" + member.Relative + "/" + file.Path)
			w.Write(data)
		}
	}
	data, _ := json.Marshal(legacy)
	w, _ := z.Create("dsh-space-manifest.json")
	w.Write(data)
	z.Close()
	f.Close()
	if e = s.RestoreHidden(hidden.ID); e != nil {
		t.Fatal(e)
	}
	backups, e := s.Backups()
	if e != nil || len(backups) != 1 || backups[0].ID != manual.ID {
		t.Fatal(backups, e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("旧自动 ZIP 未清理", e)
	}
}

func TestDeleteRequiresConfirmationAndPreservesSharedImagesAndManualZIP(t *testing.T) {
	s := fixture(t)
	parent := session(t, s, "session-parent", "", false)
	session(t, s, "session-child", "session-parent", false)
	other := session(t, s, "session-other", "", false)
	ref, _, imagePath := syntheticImage(t, s)
	attachSynthetic(t, parent, ref)
	attachSynthetic(t, other, ref)
	key := keyFor(t, s, "session-parent")
	manual, e := s.Backup([]string{key})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DeleteConfirmed(""); e == nil {
		t.Fatal("未预览即可删除")
	}
	preview, e := s.PreviewDelete(DeleteRequest{Kind: "conversations", Keys: []string{key}})
	if e != nil {
		t.Fatal(e)
	}
	if len(preview.Items) != 2 || preview.Images != 0 || len(catalog(t, s).Items) != 3 {
		t.Fatal("预览范围或共享图片错误", preview)
	}
	result, e := s.DeleteConfirmed(preview.Token)
	if e != nil || result.Conversations != 2 || result.Images != 0 {
		t.Fatal(result, e)
	}
	if len(catalog(t, s).Items) != 1 {
		t.Fatal("误删其他对话")
	}
	if _, e = os.Stat(imagePath); e != nil {
		t.Fatal("共享图片被删除", e)
	}
	if _, e = s.DeleteConfirmed(preview.Token); e == nil {
		t.Fatal("确认令牌可重用")
	}
	preview, e = s.PreviewDelete(DeleteRequest{Kind: "conversations", Keys: []string{keyFor(t, s, "session-other")}})
	if e != nil || preview.Images != 1 {
		t.Fatal(preview, e)
	}
	result, e = s.DeleteConfirmed(preview.Token)
	if e != nil || result.Images != 1 {
		t.Fatal(result, e)
	}
	if _, e = os.Stat(imagePath); !os.IsNotExist(e) {
		t.Fatal("无共享引用的图片未删除", e)
	}
	backups, e := s.Backups()
	if e != nil || len(backups) != 1 || backups[0].ID != manual.ID {
		t.Fatal("手动备份被删除", e)
	}
}

func TestWorkspaceDeletionIncludesHiddenAndPreservesProjectFolder(t *testing.T) {
	s := fixture(t)
	code := filepath.Join(t.TempDir(), "真实项目文件夹")
	os.MkdirAll(code, 0700)
	sentinel := filepath.Join(code, "code.txt")
	os.WriteFile(sentinel, []byte("保留项目文件"), 0600)
	other := filepath.Join(t.TempDir(), "其他项目")
	os.MkdirAll(other, 0700)
	rehomeSynthetic(t, s, "session-live", code)
	rehomeSynthetic(t, s, "session-hidden", code)
	rehomeSynthetic(t, s, "session-other", other)
	registerSynthetic(t, s, map[string]any{"project": workspaceFixture(code, "自定义工作区", "session-live", "session-hidden"), "other": workspaceFixture(other, "其他工作区", "session-other")}, []string{"project", "other"})
	if _, e := s.Hide([]string{keyFor(t, s, "session-hidden")}); e != nil {
		t.Fatal(e)
	}
	preview, e := s.PreviewDelete(DeleteRequest{Kind: "workspace", WorkspaceKey: workspaceKey("project")})
	if e != nil {
		t.Fatal(e)
	}
	hiddenCount := 0
	for _, item := range preview.Items {
		if item.Hidden {
			hiddenCount++
		}
	}
	if len(preview.Items) != 2 || hiddenCount != 1 || preview.WorkspaceTitle != "自定义工作区" {
		t.Fatal(preview)
	}
	result, e := s.DeleteConfirmed(preview.Token)
	if e != nil || result.Conversations != 2 {
		t.Fatal(result, e)
	}
	if len(catalog(t, s).Items) != 1 {
		t.Fatal("误删其他工作区")
	}
	if data, e := os.ReadFile(sentinel); e != nil || string(data) != "保留项目文件" {
		t.Fatal("代码文件夹被删除", e)
	}
	hidden, e := s.Hidden()
	if e != nil || len(hidden) != 0 {
		t.Fatal("遗留已删除隐藏记录", hidden, e)
	}
	doc, raw, e := s.workspaceDocument()
	if e != nil || len(doc.Global.WorkspaceIDs) != 1 || doc.Global.WorkspaceIDs[0] != "other" || len(doc.Global.Archived) != 0 || len(doc.Global.Pinned) != 0 {
		t.Fatal(doc, e)
	}
	if !strings.Contains(string(raw), "原样保留") || !strings.Contains(string(raw), "custom") {
		t.Fatal("未知登记字段被改写")
	}
}

func TestDeleteRejectsChangedScopeExpiredTokenAndRunningHarness(t *testing.T) {
	for _, mode := range []string{"changed-log", "new-shared-reference", "expired", "running"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			dir := session(t, s, "session-selected", "", false)
			other := session(t, s, "session-other", "", false)
			ref, _, path := syntheticImage(t, s)
			attachSynthetic(t, dir, ref)
			preview, e := s.PreviewDelete(DeleteRequest{Kind: "conversations", Keys: []string{keyFor(t, s, "session-selected")}})
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "changed-log":
				renameSynthetic(t, dir, "新备注")
			case "new-shared-reference":
				attachSynthetic(t, other, ref)
			case "expired":
				plan := s.deletePlans[preview.Token]
				plan.At = time.Now().Add(-6 * time.Minute)
				s.deletePlans[preview.Token] = plan
			case "running":
				s.probe = func() error { return fmt.Errorf("请先退出 DeepSeek Harness") }
			}
			if _, e = s.DeleteConfirmed(preview.Token); e == nil {
				t.Fatal("变化或未授权状态仍删除")
			}
			if len(catalog(t, s).Items) != 2 {
				t.Fatal("失败后会话减少")
			}
			if _, e = os.Stat(path); e != nil {
				t.Fatal("失败后图片被删除", e)
			}
		})
	}
}

func TestWorkspaceTitlesPathsOrderAndEmptyWorkspaceDeletion(t *testing.T) {
	s := fixture(t)
	cwd := `C:\合成项目\工作目录`
	session(t, s, "session-live", "", false)
	registerSynthetic(t, s, map[string]any{"empty": workspaceFixture(`D:\完整路径\default-workspace`, "default-workspace"), "project": workspaceFixture(cwd, "我的工作区", "session-live")}, []string{"empty", "project"})
	c := catalog(t, s)
	if len(c.Workspaces) != 2 || c.Workspaces[0].Title != "默认工作区" || c.Workspaces[0].Path != `D:\完整路径\default-workspace` || c.Items[0].WorkspaceKey != workspaceKey("project") || !c.Items[0].Pinned {
		t.Fatal(c)
	}
	preview, e := s.PreviewDelete(DeleteRequest{Kind: "workspace", WorkspaceKey: workspaceKey("empty")})
	if e != nil || len(preview.Items) != 0 {
		t.Fatal(preview, e)
	}
	if _, e = s.DeleteConfirmed(preview.Token); e != nil {
		t.Fatal(e)
	}
	c = catalog(t, s)
	if len(c.Workspaces) != 1 || len(c.Items) != 1 {
		t.Fatal(c)
	}
}

func TestPermanentDeleteProtectsImagesReferencedByHiddenConversation(t *testing.T) {
	s := fixture(t)
	live := session(t, s, "session-live", "", false)
	other := session(t, s, "session-hidden", "", false)
	ref, _, path := syntheticImage(t, s)
	attachSynthetic(t, live, ref)
	attachSynthetic(t, other, ref)
	hidden, e := s.Hide([]string{keyFor(t, s, "session-hidden")})
	if e != nil {
		t.Fatal(e)
	}
	preview, e := s.PreviewDelete(DeleteRequest{Kind: "conversations", Keys: []string{keyFor(t, s, "session-live")}})
	if e != nil || preview.Images != 0 {
		t.Fatal(preview, e)
	}
	if _, e = s.DeleteConfirmed(preview.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("隐藏对话的图片被误删", e)
	}
	preview, e = s.PreviewDelete(DeleteRequest{Kind: "hidden", HiddenID: hidden.ID})
	if e != nil || preview.Images != 1 {
		t.Fatal(preview, e)
	}
	if _, e = s.DeleteConfirmed(preview.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("最后的引用删除后图片仍保留", e)
	}
	rows, e := s.Hidden()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
