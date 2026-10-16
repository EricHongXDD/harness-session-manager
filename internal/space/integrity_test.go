package space

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHeaderRequiredFieldsAndUnicodePreview(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-unicode", "", false)
	path := filepath.Join(dir, "session.v4.jsonl")
	blob, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(string(blob), "\n")
	var header map[string]any
	json.Unmarshal([]byte(lines[0]), &header)
	for _, field := range []string{"createdAt", "id", "isSeeded", "delegationDepth"} {
		original := header[field]
		delete(header, field)
		b, _ := json.Marshal(header)
		os.WriteFile(path, append(b, '\n'), 0600)
		if _, e = readHeader(path, 4); e == nil {
			t.Fatal("缺少字段被接受", field)
		}
		header[field] = original
	}
	event, _ := json.Marshal(map[string]any{"seq": 0, "type": "user/message", "data": map[string]any{"text": strings.Repeat("中", 30000)}})
	b, _ := json.Marshal(header)
	os.WriteFile(path, append(append(b, '\n'), append(event, '\n')...), 0600)
	c := catalog(t, s)
	p, e := s.Read(c.Items[0].Key, 0)
	if e != nil || len(p.Items) != 1 || !utf8.ValidString(p.Items[0].Text) || !strings.Contains(p.Items[0].Text, "事件过长") {
		t.Fatal("UTF-8截断错误", e)
	}
}
func TestHiddenMeasuresOnlyExistingMovedFiles(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-measure", "", false)
	key := catalog(t, s).Items[0].Key
	record, e := s.Hide([]string{key})
	if e != nil {
		t.Fatal(e)
	}
	records, e := s.Hidden()
	if e != nil || len(records[0].Moved) != 1 || records[0].Bytes <= 0 {
		t.Fatal(records, e)
	}
	if e = os.MkdirAll(filepath.Dir(dir), 0700); e != nil {
		t.Fatal(e)
	}
	if e = moveNoReplace(filepath.Join(filepath.Dir(record.Path), "sessions", filepath.FromSlash(record.Members[0].Relative)), dir); e != nil {
		t.Fatal(e)
	}
	records, e = s.Hidden()
	if e != nil || records[0].Bytes != 0 || len(records[0].Moved) != 0 || records[0].State != "needs_attention" {
		t.Fatal("计划大小冒充当前占用", records, e)
	}
	if e = s.RestoreHidden(record.ID); e != nil {
		t.Fatal("可核对已移回状态", e)
	}
}
func TestScanCannotStartDuringMutation(t *testing.T) {
	s := fixture(t)
	s.operation.Lock()
	defer s.operation.Unlock()
	if state := s.StartScan(); state.Running {
		t.Fatal("变更期间启动扫描")
	}
}
func TestDuplicateBackupMembersRefusedBeforeSourceWrite(t *testing.T) {
	s := fixture(t)
	session(t, s, "session-duplicate", "", false)
	record, e := s.Backup([]string{catalog(t, s).Items[0].Key})
	if e != nil {
		t.Fatal(e)
	}
	record.Members = append(record.Members, record.Members[0])
	f, e := os.Create(record.Path)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("dsh-space-manifest.json")
	b, _ := json.Marshal(record)
	w.Write(b)
	z.Close()
	f.Close()
	if e = s.RestoreBackup(record.ID); e == nil || !strings.Contains(e.Error(), "目录重复") {
		t.Fatal("重复成员未拒绝", e)
	}
	if len(catalog(t, s).Items) != 1 {
		t.Fatal("源被改动")
	}
}

// 设置落盘失败或选到同名普通文件时，旧来源及扫描视图必须保留。
func TestSourceSwitchFailureKeepsExistingSource(t *testing.T) {
	s := fixture(t)
	old := s.Settings().Source
	other := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(filepath.Join(other, "sessions"), 0700)
	os.Mkdir(filepath.Join(s.Settings().Storage, "settings.json"), 0700)
	if e := s.SetSource(other); e == nil {
		t.Fatal("设置保存失败未报告")
	}
	if s.Settings().Source != old || s.ScanState().Root != old {
		t.Fatal("失败时已切换来源")
	}
	invalid := filepath.Join(t.TempDir(), "invalid")
	os.Mkdir(invalid, 0700)
	os.WriteFile(filepath.Join(invalid, "sessions"), []byte("not a directory"), 0600)
	if e := s.SetSource(invalid); e == nil {
		t.Fatal("同名普通文件被接受为会话目录")
	}
	if s.Settings().Source != old {
		t.Fatal("无效来源替换了旧来源")
	}
}
