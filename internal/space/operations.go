package space

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const maxFiles = 100000
const maxBackupBytes = int64(20 << 30)

func digestFile(path string, writer io.Writer) (File, error) {
	before, e := regular(path)
	if e != nil {
		return File{}, e
	}
	f, e := os.Open(path)
	if e != nil {
		return File{}, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		return File{}, fmt.Errorf("文件身份已改变")
	}
	h := sha256.New()
	sink := io.Writer(h)
	if writer != nil {
		sink = io.MultiWriter(writer, h)
	}
	n, e := io.Copy(sink, io.LimitReader(f, maxBackupBytes+1))
	if e != nil {
		return File{}, e
	}
	after, e := f.Stat()
	if e != nil || n != before.Size() || n > maxBackupBytes || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		return File{}, fmt.Errorf("文件变化或超过备份限制，请刷新")
	}
	return File{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func snapshotMember(root string, c Conversation) (Member, error) {
	member := Member{Relative: c.Relative, ID: c.ID, Title: c.Title, Files: []File{}}
	dir := filepath.Join(root, filepath.FromSlash(c.Relative))
	if !safeRelative(c.Relative) || !within(root, dir) {
		return member, fmt.Errorf("会话路径无效")
	}
	if e := noLinks(dir); e != nil {
		return member, e
	}
	total := int64(0)
	e := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || reparse(info) {
			return fmt.Errorf("会话含链接，未执行操作")
		}
		if entry.IsDir() {
			return nil
		}
		if len(member.Files) >= maxFiles {
			return fmt.Errorf("备份文件数量超限")
		}
		file, e := digestFile(path, nil)
		if e != nil {
			return e
		}
		relative, _ := filepath.Rel(dir, path)
		file.Path = filepath.ToSlash(relative)
		if !safeRelative(file.Path) {
			return fmt.Errorf("会话文件路径无效")
		}
		total += file.Size
		if total > maxBackupBytes {
			return fmt.Errorf("单批备份最大 20 GiB")
		}
		member.Files = append(member.Files, file)
		return nil
	})
	return member, e
}
func (s *Service) selection(keys []string, family bool) ([]Conversation, error) {
	if len(keys) == 0 || len(keys) > 100 {
		return nil, fmt.Errorf("请选择 1 至 100 个对话")
	}
	catalog, e := s.List()
	if e != nil {
		return nil, e
	}
	byKey := map[string]Conversation{}
	nativeCount := map[string]int{}
	for _, c := range catalog.Items {
		byKey[c.Key] = c
		nativeCount[c.ID]++
	}
	selected := map[string]bool{}
	native := map[string]bool{}
	for _, key := range keys {
		c, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("部分对话已变化，请刷新")
		}
		if family && (c.Warning != "" || nativeCount[c.ID] != 1) {
			return nil, fmt.Errorf("对话格式或身份不明确，不能隐藏：%s", c.Title)
		}
		selected[key] = true
		native[c.ID] = true
	}
	if family {
		if len(catalog.Warnings) > 0 {
			return nil, fmt.Errorf("来源未完整枚举，无法核对会话组，请先处理来源提醒")
		}
		for _, c := range catalog.Items {
			if c.Warning != "" {
				return nil, fmt.Errorf("来源含未识别会话，无法完整核对子对话；可先备份")
			}
		}
		changed := true
		for changed {
			changed = false
			for _, c := range catalog.Items {
				// 整个父子会话组一起移动，避免原页面保留指向隐藏子对话的目录关系。
				if selected[c.Key] && c.Parent != "" && !native[c.Parent] {
					for _, parent := range catalog.Items {
						if parent.ID == c.Parent {
							selected[parent.Key] = true
							native[parent.ID] = true
							changed = true
						}
					}
				}
				if c.Parent != "" && native[c.Parent] && !selected[c.Key] {
					selected[c.Key] = true
					native[c.ID] = true
					changed = true
				}
			}
		}
	}
	result := []Conversation{}
	for key := range selected {
		result = append(result, byKey[key])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Relative < result[j].Relative })
	return result, nil
}
func (s *Service) prepare(keys []string, family bool) (Record, error) {
	record := Record{FormatVersion: 2, ID: newID(), Kind: "backup", State: "preparing", Created: time.Now().UTC(), Members: []Member{}, Moved: []string{}}
	if e := s.guard(); e != nil {
		return record, e
	}
	cfg := s.Settings()
	record.Source = cfg.Source
	identity, _, e := nativeStat(filepath.Join(cfg.Source, "sessions"))
	if e != nil {
		return record, e
	}
	record.SourceIdentity = identity
	selected, e := s.selection(keys, family)
	if e != nil {
		return record, e
	}
	count := 0
	total := int64(0)
	for _, c := range selected {
		m, e := snapshotMember(filepath.Join(cfg.Source, "sessions"), c)
		if e != nil {
			return record, e
		}
		for _, f := range m.Files {
			count++
			total += f.Size
		}
		if count > maxFiles || total > maxBackupBytes {
			return record, fmt.Errorf("单批最多 100000 文件、20 GiB")
		}
		record.Members = append(record.Members, m)
	}
	if !family {
		record.Attachments, e = s.snapshotImages(selected)
		if e != nil {
			return record, e
		}
	}
	for _, file := range record.Attachments {
		count++
		total += file.Size
	}
	if count > maxFiles || total > maxBackupBytes {
		return record, fmt.Errorf("含图片的备份超过 100000 文件或 20 GiB 限制")
	}
	return record, nil
}
func saveRecord(record Record) error {
	data, e := json.MarshalIndent(record, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(record.Path, data)
}
func verifyMember(root string, member Member) error {
	current, e := snapshotMember(root, Conversation{Relative: member.Relative, ID: member.ID, Title: member.Title})
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(current.Files, member.Files) {
		return fmt.Errorf("会话目录内容已变化，请重新操作")
	}
	return nil
}
func (s *Service) Backup(keys []string) (Record, error) {
	if e := s.lock(); e != nil {
		return Record{}, e
	}
	defer s.operation.Unlock()
	record, e := s.prepare(keys, false)
	if e != nil {
		return record, e
	}
	return s.backup(record)
}
func (s *Service) backup(record Record) (Record, error) {
	cfg := s.Settings()
	dir := filepath.Join(cfg.Storage, "backups")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return record, e
	}
	path := filepath.Join(dir, record.Created.Format("20060102-150405")+"-"+record.ID+".zip")
	partial := path + ".partial"
	f, e := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return record, e
	}
	defer os.Remove(partial)
	archive := zip.NewWriter(f)
	failed := true
	defer func() {
		if failed {
			archive.Close()
			f.Close()
		}
	}()
	for _, member := range record.Members {
		for _, expected := range member.Files {
			entry, e := archive.Create("sessions/" + member.Relative + "/" + expected.Path)
			if e != nil {
				return record, e
			}
			actual, e := digestFile(filepath.Join(cfg.Source, "sessions", filepath.FromSlash(member.Relative), filepath.FromSlash(expected.Path)), entry)
			if e != nil || actual.SHA256 != expected.SHA256 || actual.Size != expected.Size {
				return record, fmt.Errorf("备份期间来源变化，原会话保持：%v", e)
			}
		}
	}
	for _, expected := range record.Attachments {
		entry, err := archive.Create(expected.Path)
		if err != nil {
			return record, err
		}
		actual, err := digestFile(filepath.Join(cfg.Source, filepath.FromSlash(expected.Path)), entry)
		if err != nil || actual.SHA256 != expected.SHA256 || actual.Size != expected.Size {
			return record, fmt.Errorf("备份期间图片变化，未发布备份")
		}
	}
	record.State = "ready"
	record.Kind = "backup"
	record.Path = path
	manifest, _ := json.Marshal(record)
	entry, e := archive.Create("dsh-space-manifest.json")
	if e != nil {
		return record, e
	}
	if _, e = entry.Write(manifest); e != nil {
		return record, e
	}
	if e = archive.Close(); e != nil {
		return record, e
	}
	if e = f.Sync(); e != nil {
		return record, e
	}
	if e = f.Close(); e != nil {
		return record, e
	}
	failed = false
	for _, member := range record.Members {
		if e = verifyMember(filepath.Join(cfg.Source, "sessions"), member); e != nil {
			return record, e
		}
	}
	if e = s.guard(); e != nil {
		return record, e
	}
	if e = moveNoReplace(partial, path); e != nil {
		return record, e
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return record, fmt.Errorf("备份发布后无法核对，请刷新备份列表：%w", statErr)
	}
	record.Bytes = info.Size()
	return record, nil
}
func (s *Service) Hide(keys []string) (Record, error) { return s.hide(keys, nil) }
func (s *Service) HideConfirmed(request HideRequest) (Record, error) {
	return s.hide(request.Keys, &request)
}
func (s *Service) hide(keys []string, expected *HideRequest) (Record, error) {
	if e := s.lock(); e != nil {
		return Record{}, e
	}
	defer s.operation.Unlock()
	record, e := s.prepare(keys, true)
	if e != nil {
		return record, e
	}
	if expected != nil {
		actual := []string{}
		for _, member := range record.Members {
			actual = append(actual, conversationKey(member.Relative))
		}
		wanted := append([]string{}, expected.ExpectedKeys...)
		sort.Strings(actual)
		sort.Strings(wanted)
		if expected.Source != record.Source || expected.Identity != record.SourceIdentity || !reflect.DeepEqual(actual, wanted) {
			return record, fmt.Errorf("来源或会话组已变化，请重新预览隐藏清单")
		}
	}
	cfg := s.Settings()
	record.Kind = "hidden"
	record.State = "moving"
	dir := filepath.Join(cfg.Storage, "hidden", record.ID)
	record.Path = filepath.Join(dir, "record.json")
	if e = os.MkdirAll(filepath.Join(dir, "sessions"), 0700); e != nil {
		return record, e
	}
	if e = saveRecord(record); e != nil {
		return record, e
	}
	for _, member := range record.Members {
		if e = s.guard(); e != nil {
			break
		}
		if e = verifyMember(filepath.Join(cfg.Source, "sessions"), member); e != nil {
			break
		}
		from := filepath.Join(cfg.Source, "sessions", filepath.FromSlash(member.Relative))
		to := filepath.Join(dir, "sessions", filepath.FromSlash(member.Relative))
		if e = os.MkdirAll(filepath.Dir(to), 0700); e != nil {
			break
		}
		if e = moveNoReplace(from, to); e != nil {
			e = fmt.Errorf("移动失败（隐藏区需与来源同卷），原目录保持：%w", e)
			break
		}
		record.Moved = append(record.Moved, member.Relative)
		if e = saveRecord(record); e != nil {
			break
		}
	}
	if e != nil {
		record.State = "needs_attention"
		record.Error = e.Error()
		_ = saveRecord(record)
		return record, fmt.Errorf("隐藏未全部完成，已移动 %d 个目录；请在隐藏页恢复。%w", len(record.Moved), e)
	}
	record.State = "hidden"
	if e = saveRecord(record); e != nil {
		return record, e
	}
	return record, nil
}
func validRecordID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, e := hex.DecodeString(id)
	return e == nil
}
func (s *Service) Hidden() ([]Record, error) {
	result := []Record{}
	root := filepath.Join(s.Settings().Storage, "hidden")
	dirs, e := os.ReadDir(root)
	if os.IsNotExist(e) {
		return result, nil
	}
	if e != nil {
		return result, e
	}
	for _, dir := range dirs {
		if !dir.IsDir() || !validRecordID(dir.Name()) {
			continue
		}
		path := filepath.Join(root, dir.Name(), "record.json")
		info, e := regular(path)
		if e != nil || info.Size() > 32<<20 {
			continue
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return result, e
		}
		var record Record
		if json.Unmarshal(data, &record) != nil || record.ID != dir.Name() || record.Kind != "hidden" {
			continue
		}
		record.Path = path
		record.Bytes = 0
		record.Moved = []string{}
		for _, member := range record.Members {
			if !safeRelative(member.Relative) {
				record.State = "needs_attention"
				record.Error = "隐藏记录含无效路径"
				continue
			}
			memberDir := filepath.Join(filepath.Dir(path), "sessions", filepath.FromSlash(member.Relative))
			if _, err := os.Lstat(memberDir); os.IsNotExist(err) {
				continue
			}
			record.Moved = append(record.Moved, member.Relative)
			err := filepath.WalkDir(memberDir, func(p string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				info, err := os.Lstat(p)
				if err != nil {
					return err
				}
				if reparse(info) || info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("隐藏区含链接，大小未完整统计")
				}
				if info.Mode().IsRegular() {
					record.Bytes += info.Size()
				}
				return nil
			})
			if err != nil {
				record.State = "needs_attention"
				record.Error = "隐藏区大小未完整统计，请核对原始目录"
			}
		}
		if (record.State == "hidden" && len(record.Moved) != len(record.Members)) || (record.State == "restored" && len(record.Moved) > 0) {
			record.State = "needs_attention"
			record.Error = "实际隐藏目录与记录状态不符，请核对恢复"
		}
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}
func (s *Service) RestoreHidden(id string) error {
	if e := s.lock(); e != nil {
		return e
	}
	defer s.operation.Unlock()
	if !validRecordID(id) {
		return fmt.Errorf("隐藏记录 ID 无效")
	}
	if e := s.guard(); e != nil {
		return e
	}
	records, e := s.Hidden()
	if e != nil {
		return e
	}
	var record *Record
	for i := range records {
		if records[i].ID == id {
			record = &records[i]
			break
		}
	}
	if record == nil {
		return fmt.Errorf("隐藏记录不存在")
	}
	cfg := s.Settings()
	if record.Source != cfg.Source {
		return fmt.Errorf("请先切换回隐藏记录的原来源目录：%s", record.Source)
	}
	identity, _, e := nativeStat(filepath.Join(cfg.Source, "sessions"))
	if e != nil || identity != record.SourceIdentity {
		return fmt.Errorf("原来源目录身份已变化，未执行恢复")
	}
	hiddenRoot := filepath.Join(filepath.Dir(record.Path), "sessions")
	// 即使程序在移动后尚未写入进度便退出，也以清单和现存载体重新核对。
	for _, member := range record.Members {
		if !safeRelative(member.Relative) {
			return fmt.Errorf("隐藏记录路径无效")
		}
		from := filepath.Join(hiddenRoot, filepath.FromSlash(member.Relative))
		to := filepath.Join(cfg.Source, "sessions", filepath.FromSlash(member.Relative))
		if _, e = os.Lstat(from); os.IsNotExist(e) {
			if e = verifyMember(filepath.Join(cfg.Source, "sessions"), member); e != nil {
				return fmt.Errorf("部分恢复状态未知：%w", e)
			}
			continue
		}
		if e = verifyMember(hiddenRoot, member); e != nil {
			return e
		}
		if _, e = os.Lstat(to); !os.IsNotExist(e) {
			return fmt.Errorf("原位置已有同名会话，不覆盖：%s", member.ID)
		}
	}
	for _, member := range record.Members {
		from := filepath.Join(hiddenRoot, filepath.FromSlash(member.Relative))
		if _, e = os.Lstat(from); os.IsNotExist(e) {
			continue
		}
		to := filepath.Join(cfg.Source, "sessions", filepath.FromSlash(member.Relative))
		if e = s.guard(); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(to), 0700); e != nil {
			return e
		}
		if e = noLinks(filepath.Dir(to)); e != nil {
			return e
		}
		if e = moveNoReplace(from, to); e != nil {
			return e
		}
	}
	record.State = "restored"
	record.Error = ""
	if e = saveRecord(*record); e != nil {
		return e
	}
	if e = s.cleanupRestoredHidden(*record); e != nil {
		record.State = "cleanup_pending"
		record.Error = e.Error()
		_ = saveRecord(*record)
		return e
	}
	return nil
}

// 恢复后只移除已空的隐藏容器，以及旧版为同一次隐藏自动生成的 ZIP。
func (s *Service) cleanupRestoredHidden(record Record) error {
	backups, e := s.Backups()
	if e != nil {
		return e
	}
	for _, backup := range backups {
		if backup.ID == record.ID && backup.Source == record.Source && backup.SourceIdentity == record.SourceIdentity && backup.Created.Equal(record.Created) && reflect.DeepEqual(backup.Members, record.Members) {
			if _, e = regular(backup.Path); e != nil {
				return e
			}
			if e = os.Remove(backup.Path); e != nil {
				return fmt.Errorf("会话已恢复，旧自动 ZIP 清理失败：%w", e)
			}
		}
	}
	base := filepath.Dir(record.Path)
	root := filepath.Join(s.Settings().Storage, "hidden")
	if base == root || !within(root, base) || filepath.Base(base) != record.ID {
		return fmt.Errorf("隐藏清理路径无效")
	}
	if e = noLinks(base); e != nil {
		return e
	}
	// 不递归删除：仅删除空项目目录、空 sessions 和本次记录，未知文件会保留并报错。
	sessions := filepath.Join(base, "sessions")
	entries, e := os.ReadDir(sessions)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return fmt.Errorf("隐藏区仍有未知文件，未清理")
		}
		if e = os.Remove(filepath.Join(sessions, entry.Name())); e != nil {
			return fmt.Errorf("隐藏区仍有内容，未清理：%w", e)
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
func (s *Service) Backups() ([]Record, error) {
	result := []Record{}
	root := filepath.Join(s.Settings().Storage, "backups")
	entries, e := os.ReadDir(root)
	if os.IsNotExist(e) {
		return result, nil
	}
	if e != nil {
		return result, e
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		record, e := readBackup(path)
		if e != nil {
			continue
		}
		record.Path = path
		info, _ := os.Stat(path)
		record.Bytes = info.Size()
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}
func readBackup(path string) (Record, error) {
	var record Record
	if _, e := regular(path); e != nil {
		return record, e
	}
	archive, e := zip.OpenReader(path)
	if e != nil {
		return record, e
	}
	defer archive.Close()
	if len(archive.File) > maxFiles+1 {
		return record, fmt.Errorf("备份文件数超限")
	}
	for _, entry := range archive.File {
		if entry.Name == "dsh-space-manifest.json" {
			if entry.UncompressedSize64 > 32<<20 {
				return record, fmt.Errorf("备份清单过大")
			}
			r, e := entry.Open()
			if e != nil {
				return record, e
			}
			data, e := io.ReadAll(io.LimitReader(r, 32<<20+1))
			r.Close()
			if e != nil || json.Unmarshal(data, &record) != nil || record.Kind != "backup" || !validRecordID(record.ID) {
				return record, fmt.Errorf("备份清单无效")
			}
			return record, nil
		}
	}
	return record, fmt.Errorf("不是 DSH Space 备份")
}
