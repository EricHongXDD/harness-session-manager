package space

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	base := t.TempDir()
	source := filepath.Join(base, "harness")
	if e := os.MkdirAll(filepath.Join(source, "sessions"), 0700); e != nil {
		t.Fatal(e)
	}
	s, e := New(source, filepath.Join(base, "app"), func() error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func session(t *testing.T, s *Service, id, parent string, compressed bool) string {
	t.Helper()
	dir := filepath.Join(s.Settings().Source, "sessions", ProjectDirectory(`C:\合成项目\工作目录`), id)
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	h := Header{Type: "session", Version: 4, ID: id, CWD: `C:\合成项目\工作目录`, Created: 1792000000000, Parent: parent}
	data, _ := json.Marshal(h)
	data = append(data, '\n')
	events := []map[string]any{
		{"seq": 0, "time": h.Created, "type": "user/message", "surfaceOp": "append", "data": map[string]any{"id": "user-1", "role": "user", "source": map[string]any{"kind": "user"}, "content": []any{map[string]any{"type": "text", "text": "完整中文与 Emoji 🐳"}}}},
		{"seq": 1, "time": h.Created + 1, "type": "turn/start", "data": map[string]any{"turn": 1}},
		{"seq": 2, "time": h.Created + 2, "type": "step/start", "data": map[string]any{"turn": 1, "step": 1}},
		{"seq": 3, "time": h.Created + 3, "type": "assistant/message", "surfaceOp": "append", "data": map[string]any{"turn": 1, "step": 1, "stream": []any{}, "message": map[string]any{"id": "assistant-1", "role": "assistant", "source": map[string]any{"kind": "model", "provider": "demo", "model": "synthetic"}, "content": []any{map[string]any{"type": "text", "text": "合成助手答复"}}}}},
		{"seq": 4, "time": h.Created + 4, "type": "step/end", "data": map[string]any{"turn": 1, "step": 1}},
		{"seq": 5, "time": h.Created + 5, "type": "turn/end", "data": map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}},
	}
	for _, e := range events {
		line, _ := json.Marshal(e)
		data = append(data, append(line, '\n')...)
	}
	path := filepath.Join(dir, "session.v4.jsonl")
	if compressed {
		path += ".zstd"
		encoder, e := zstd.NewWriter(nil)
		if e != nil {
			t.Fatal(e)
		}
		split := bytes.IndexByte(data, '\n') + 1
		data = append(encoder.EncodeAll(data[:split], nil), encoder.EncodeAll(data[split:], nil)...)
		encoder.Close()
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	cache := map[string]any{"version": 7, "record": map[string]any{"identity": map[string]any{"formatVersion": 4, "createdAt": h.Created, "cwd": h.CWD}, "rows": map[string]any{"title": map[string]any{"ver": 1, "seq": 1, "val": "中文会话 " + id}}}}
	blob, _ := json.Marshal(cache)
	cachePath := filepath.Join(s.Settings().Source, "storages", "session_projcache", "sessions", id+".json")
	os.MkdirAll(filepath.Dir(cachePath), 0700)
	os.WriteFile(cachePath, blob, 0600)
	return dir
}
func catalog(t *testing.T, s *Service) Catalog {
	t.Helper()
	c, e := s.List()
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestV4PlainCompressedTitleAndRead(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprint(compressed), func(t *testing.T) {
			s := fixture(t)
			session(t, s, "session-main", "", compressed)
			c := catalog(t, s)
			if len(c.Items) != 1 || c.Items[0].Warning != "" || c.Items[0].Title != "中文会话 session-main" {
				t.Fatal(c)
			}
			page, e := s.Read(c.Items[0].Key, 0)
			if e != nil || len(page.Items) != 6 || page.Items[0].Text != "完整中文与 Emoji 🐳" || page.HasMore {
				t.Fatal(page, e)
			}
		})
	}
}
func TestFutureGenerationNeverFallsBack(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-future", "", false)
	os.WriteFile(filepath.Join(dir, "session.v5.jsonl"), []byte("{}\n"), 0600)
	c := catalog(t, s)
	if c.Items[0].Version != 5 || c.Items[0].Warning == "" {
		t.Fatal(c)
	}
	if _, e := s.Hide([]string{c.Items[0].Key}); e == nil {
		t.Fatal("未来格式被移动")
	}
}
func TestHideFamilyBackupRestorePreservesEveryByte(t *testing.T) {
	s := fixture(t)
	parent := session(t, s, "session-parent", "", true)
	child := session(t, s, "session-child", "session-parent", true)
	os.WriteFile(filepath.Join(parent, "附件.bin"), []byte{0, 1, 2, 255}, 0600)
	before := catalog(t, s)
	snapshots := map[string]Member{}
	for _, c := range before.Items {
		m, e := snapshotMember(filepath.Join(s.Settings().Source, "sessions"), c)
		if e != nil {
			t.Fatal(e)
		}
		snapshots[c.ID] = m
	}
	var key string
	for _, c := range before.Items {
		if c.ID == "session-parent" {
			key = c.Key
		}
	}
	manualKeys := []string{}
	for _, c := range before.Items {
		manualKeys = append(manualKeys, c.Key)
	}
	if _, e := s.Backup(manualKeys); e != nil {
		t.Fatal(e)
	}
	hidden, e := s.Hide([]string{key})
	if e != nil || len(hidden.Members) != 2 || hidden.State != "hidden" {
		t.Fatal(hidden, e)
	}
	if len(catalog(t, s).Items) != 0 {
		t.Fatal("隐藏后仍被会话目录枚举")
	}
	for _, dir := range []string{parent, child} {
		if _, e = os.Stat(dir); !os.IsNotExist(e) {
			t.Fatal("原目录仍存在", e)
		}
	}
	backups, e := s.Backups()
	if e != nil || len(backups) != 1 {
		t.Fatal(backups, e)
	}
	if e = s.RestoreHidden(hidden.ID); e != nil {
		t.Fatal(e)
	}
	for _, c := range catalog(t, s).Items {
		m, e := snapshotMember(filepath.Join(s.Settings().Source, "sessions"), c)
		if e != nil || !reflect.DeepEqual(m.Files, snapshots[c.ID].Files) {
			t.Fatal("恢复后原文不一致", m, e)
		}
	}
	// 移除的仅为本测试合成来源，再使用独立ZIP恢复；重复恢复不覆盖相同目录。
	for _, dir := range []string{parent, child} {
		if e = os.RemoveAll(dir); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.RestoreBackup(backups[0].ID); e != nil {
		t.Fatal(e)
	}
	if e = s.RestoreBackup(backups[0].ID); e != nil {
		t.Fatal("幂等恢复失败", e)
	}
	if len(catalog(t, s).Items) != 2 {
		t.Fatal("备份恢复缺项")
	}
}
func TestRunningWriterLeavesSourceAndStorageUntouched(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-active", "", false)
	c := catalog(t, s).Items[0]
	before, e := snapshotMember(filepath.Join(s.Settings().Source, "sessions"), c)
	if e != nil {
		t.Fatal(e)
	}
	s.probe = func() error { return fmt.Errorf("仍运行") }
	if _, e = s.Hide([]string{c.Key}); e == nil {
		t.Fatal("运行中仍允许隐藏")
	}
	if _, e = s.Backup([]string{c.Key}); e == nil {
		t.Fatal("运行中仍允许备份")
	}
	after, e := snapshotMember(filepath.Join(s.Settings().Source, "sessions"), c)
	if e != nil || !reflect.DeepEqual(before.Files, after.Files) {
		t.Fatal("来源变化", dir, e)
	}
	entries, _ := os.ReadDir(s.Settings().Storage)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}
func TestHiddenConflictAndInterruptedProgressAreRecoverable(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-conflict", "", false)
	key := catalog(t, s).Items[0].Key
	record, e := s.Hide([]string{key})
	if e != nil {
		t.Fatal(e)
	}
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "sentinel.txt"), []byte("不可覆盖"), 0600)
	if e = s.RestoreHidden(record.ID); e == nil {
		t.Fatal("覆盖冲突目录")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "sentinel.txt")); string(data) != "不可覆盖" {
		t.Fatal("旁观文件改变")
	}
	os.RemoveAll(dir)
	record.State = "moving"
	record.Moved = []string{}
	if e = saveRecord(record); e != nil {
		t.Fatal(e)
	}
	if e = s.RestoreHidden(record.ID); e != nil {
		t.Fatal("未记录最后移动也应可核对恢复", e)
	}
}
func TestRelativePathsAndMoveNoOverwrite(t *testing.T) {
	for _, path := range []string{"../x", "x/../y", "C:/x", "/x", "a\\b", "a//b", "a/NUL.txt", "a/file. ", "a/CON"} {
		if safeRelative(path) {
			t.Fatal("危险路径被接受", path)
		}
	}
	if !safeRelative("--project--/session-main/中文.bin") {
		t.Fatal("正常路径被拒")
	}
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	os.Mkdir(a, 0700)
	os.Mkdir(b, 0700)
	os.WriteFile(filepath.Join(b, "sentinel"), []byte("keep"), 0600)
	if e := moveNoReplace(a, b); e == nil {
		t.Fatal("覆盖目标目录")
	}
	if _, e := os.Stat(a); e != nil {
		t.Fatal("来源不应消失", e)
	}
}
func TestBackupTamperedHashAndZipSlipRefusedBeforeSourceWrite(t *testing.T) {
	for _, malicious := range []bool{false, true} {
		t.Run(fmt.Sprint(malicious), func(t *testing.T) {
			s := fixture(t)
			dir := session(t, s, "session-original", "", false)
			record, e := s.Backup([]string{catalog(t, s).Items[0].Key})
			if e != nil {
				t.Fatal(e)
			}
			os.RemoveAll(dir)
			if malicious {
				record.Members[0].Relative = "../escape"
			} else {
				record.Members[0].Files[0].SHA256 = strings.Repeat("0", 64)
			}
			file, e := os.Create(record.Path)
			if e != nil {
				t.Fatal(e)
			}
			archive := zip.NewWriter(file)
			manifest, _ := json.Marshal(record)
			entry, _ := archive.Create("dsh-space-manifest.json")
			entry.Write(manifest)
			entry, _ = archive.Create("sessions/" + record.Members[0].Relative + "/" + record.Members[0].Files[0].Path)
			entry.Write([]byte("tampered"))
			archive.Close()
			file.Close()
			if e = s.RestoreBackup(record.ID); e == nil {
				t.Fatal("篡改备份被恢复")
			}
			if len(catalog(t, s).Items) != 0 {
				t.Fatal("失败后写入了来源")
			}
		})
	}
}
func TestSpaceHardlinkUnionAndSourceSwitch(t *testing.T) {
	s := fixture(t)
	root := s.Settings().Source
	a := filepath.Join(root, "large.bin")
	data := make([]byte, 12345)
	os.WriteFile(a, data, 0600)
	b := filepath.Join(root, "alias.bin")
	if e := os.Link(a, b); e != nil {
		t.Skip("文件系统不支持硬链接", e)
	}
	s.StartScan()
	s.stopScan()
	state := s.ScanState()
	if state.Canceled {
		s.StartScan()
		for s.ScanState().Running {
			time.Sleep(time.Millisecond)
		}
	}
	state = s.ScanState()
	if !state.Complete || state.Bytes != 24690 || state.Files != 2 {
		t.Fatal(state)
	}
	_, physical, e := nativeStat(a)
	if e != nil {
		t.Fatal(e)
	}
	if state.Allocated == nil || *state.Allocated != physical {
		t.Fatal("物理占用重复", state.Allocated, physical)
	}
	other := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(filepath.Join(other, "sessions"), 0700)
	if e = s.SetSource(other); e != nil {
		t.Fatal(e)
	}
	if state = s.ScanState(); state.Bytes != 0 || state.Root != other || state.Complete {
		t.Fatal("沿用旧来源统计", state)
	}
}
