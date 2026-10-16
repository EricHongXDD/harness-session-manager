package space

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) backupByID(id string) (Record, error) {
	if !validRecordID(id) {
		return Record{}, fmt.Errorf("备份 ID 无效")
	}
	records, e := s.Backups()
	if e != nil {
		return Record{}, e
	}
	var found *Record
	for i := range records {
		if records[i].ID == id {
			if found != nil {
				return Record{}, fmt.Errorf("备份 ID 重复，请在备份文件夹核对")
			}
			found = &records[i]
		}
	}
	if found == nil {
		return Record{}, fmt.Errorf("备份不存在或清单损坏")
	}
	return *found, nil
}
func (s *Service) DeleteBackup(id string) error {
	if e := s.lock(); e != nil {
		return e
	}
	defer s.operation.Unlock()
	record, e := s.backupByID(id)
	if e != nil {
		return e
	}
	if _, e = regular(record.Path); e != nil {
		return e
	}
	return os.Remove(record.Path)
}
func (s *Service) RestoreBackup(id string) error {
	if e := s.lock(); e != nil {
		return e
	}
	defer s.operation.Unlock()
	if e := s.guard(); e != nil {
		return e
	}
	record, e := s.backupByID(id)
	if e != nil {
		return e
	}
	cfg := s.Settings()
	stage, e := s.stageBackup(record)
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	// 先核对所有冲突，再发布图片和会话；不同文件绝不覆盖。
	for _, file := range record.Attachments {
		path := filepath.Join(cfg.Source, filepath.FromSlash(file.Path))
		if _, err := os.Lstat(path); err == nil {
			actual, err := digestFile(path, nil)
			if err != nil || actual.Size != file.Size || actual.SHA256 != file.SHA256 {
				return fmt.Errorf("目标已有不同图片，不会覆盖")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	root := filepath.Join(cfg.Source, "sessions")
	for _, member := range record.Members {
		destination := filepath.Join(root, filepath.FromSlash(member.Relative))
		if _, e = os.Lstat(destination); e == nil {
			if e = verifyMember(root, member); e != nil {
				return fmt.Errorf("目标已有不同会话，不会覆盖：%s", member.ID)
			}
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	for _, file := range record.Attachments {
		path := filepath.Join(cfg.Source, filepath.FromSlash(file.Path))
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		if e = s.guard(); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		if e = noLinks(filepath.Dir(path)); e != nil {
			return e
		}
		if e = moveNoReplace(filepath.Join(stage, filepath.FromSlash(file.Path)), path); e != nil {
			return e
		}
	}
	for _, member := range record.Members {
		destination := filepath.Join(root, filepath.FromSlash(member.Relative))
		if _, e = os.Lstat(destination); e == nil {
			continue
		}
		if e = s.guard(); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(destination), 0700); e != nil {
			return e
		}
		if e = noLinks(filepath.Dir(destination)); e != nil {
			return e
		}
		if e = moveNoReplace(filepath.Join(stage, "sessions", filepath.FromSlash(member.Relative)), destination); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) stageBackup(record Record) (stage string, err error) {
	archive, e := zip.OpenReader(record.Path)
	if e != nil {
		return "", e
	}
	defer archive.Close()
	expected := map[string]File{}
	aliases := map[string]bool{}
	total := int64(0)
	memberAliases := map[string]bool{}
	if len(record.Members) == 0 || len(record.Members) > maxFiles {
		return "", fmt.Errorf("备份成员无效")
	}
	for _, member := range record.Members {
		if !safeRelative(member.Relative) || len(strings.Split(member.Relative, "/")) != 2 {
			return "", fmt.Errorf("备份会话路径无效")
		}
		memberAlias := strings.ToLower(member.Relative)
		if memberAliases[memberAlias] {
			return "", fmt.Errorf("备份会话目录重复或大小写冲突")
		}
		memberAliases[memberAlias] = true
		for _, file := range member.Files {
			if !safeRelative(file.Path) || file.Size < 0 || file.Size > maxBackupBytes || len(file.SHA256) != 64 {
				return "", fmt.Errorf("备份文件清单无效")
			}
			if _, e = hex.DecodeString(file.SHA256); e != nil {
				return "", e
			}
			name := "sessions/" + member.Relative + "/" + file.Path
			alias := strings.ToLower(name)
			if aliases[alias] {
				return "", fmt.Errorf("备份路径重复或大小写冲突")
			}
			aliases[alias] = true
			expected[name] = file
			total += file.Size
			if total > maxBackupBytes || len(expected) > maxFiles {
				return "", fmt.Errorf("备份展开大小超限")
			}
		}
	}
	for _, file := range record.Attachments {
		if !validAttachmentFile(file) || file.Size < 0 || file.Size > maxBackupBytes {
			return "", fmt.Errorf("备份图片清单无效")
		}
		if aliases[file.Path] {
			return "", fmt.Errorf("图片清单重复")
		}
		aliases[file.Path] = true
		expected[file.Path] = file
		total += file.Size
		if total > maxBackupBytes || len(expected) > maxFiles {
			return "", fmt.Errorf("备份展开大小超限")
		}
	}
	parent := filepath.Join(s.Settings().Storage, "staging")
	if e = os.MkdirAll(parent, 0700); e != nil {
		return "", e
	}
	if e = noLinks(parent); e != nil {
		return "", e
	}
	stage, e = os.MkdirTemp(parent, "restore-")
	if e != nil {
		return "", e
	}
	cleanupPath := stage
	defer func() {
		if err != nil && cleanupPath != parent && within(parent, cleanupPath) {
			_ = os.RemoveAll(cleanupPath)
		}
	}()
	// 空会话目录也保留；所有成员路径已在创建暂存前通过校验。
	for _, member := range record.Members {
		if e = os.MkdirAll(filepath.Join(stage, "sessions", filepath.FromSlash(member.Relative)), 0700); e != nil {
			return "", e
		}
	}
	seen := map[string]bool{}
	manifestCount := 0
	for _, entry := range archive.File {
		if entry.Name == "dsh-space-manifest.json" {
			manifestCount++
			if manifestCount > 1 || !entry.Mode().IsRegular() {
				return "", fmt.Errorf("备份清单重复或无效")
			}
			continue
		}
		file, ok := expected[entry.Name]
		if !ok || seen[entry.Name] || !entry.Mode().IsRegular() || entry.UncompressedSize64 != uint64(file.Size) {
			return "", fmt.Errorf("备份内容与清单不一致")
		}
		seen[entry.Name] = true
		destination := filepath.Join(stage, filepath.FromSlash(entry.Name))
		if !within(stage, destination) {
			return "", fmt.Errorf("备份路径越界")
		}
		if e = os.MkdirAll(filepath.Dir(destination), 0700); e != nil {
			return "", e
		}
		reader, e := entry.Open()
		if e != nil {
			return "", e
		}
		target, e := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			reader.Close()
			return "", e
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(target, h), io.LimitReader(reader, file.Size+1))
		readErr := reader.Close()
		syncErr := target.Sync()
		closeErr := target.Close()
		if copyErr != nil || readErr != nil || syncErr != nil || closeErr != nil || n != file.Size || hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
			return "", fmt.Errorf("备份文件 SHA256 校验失败")
		}
	}
	if len(seen) != len(expected) {
		return "", fmt.Errorf("备份缺少文件")
	}
	if record.FormatVersion > 2 {
		return "", fmt.Errorf("未支持的备份版本")
	}
	if record.FormatVersion == 2 {
		for _, member := range record.Members {
			log, version, e := selectedLog(filepath.Join(stage, "sessions", filepath.FromSlash(member.Relative)))
			if e != nil || version != 4 {
				return "", fmt.Errorf("备份会话无法核对图片引用")
			}
			refs, e := logImages(log)
			if e != nil {
				return "", e
			}
			for _, ref := range refs {
				p, e := imagePath(ref.ID)
				if e != nil {
					return "", e
				}
				if _, ok := expected[p]; !ok {
					return "", fmt.Errorf("备份缺少会话引用的图片")
				}
			}
		}
	}
	return stage, nil
}

// 导入先验证整个归档，成功后仅复制到本应用备份库，不自动改写会话来源。
func (s *Service) ImportBackup(path string) (Record, error) {
	if e := s.lock(); e != nil {
		return Record{}, e
	}
	defer s.operation.Unlock()
	before, e := digestFile(path, nil)
	if e != nil {
		return Record{}, e
	}
	record, e := readBackup(path)
	if e != nil {
		return record, e
	}
	record.Path = path
	stage, e := s.stageBackup(record)
	if e != nil {
		return record, e
	}
	defer os.RemoveAll(stage)
	records, e := s.Backups()
	if e != nil {
		return record, e
	}
	for _, existing := range records {
		if existing.ID == record.ID {
			actual, e := digestFile(existing.Path, nil)
			if e == nil && actual.SHA256 == before.SHA256 && actual.Size == before.Size {
				return existing, nil
			}
			return record, fmt.Errorf("同一备份 ID 已存在不同文件，请核对原始备份")
		}
	}
	dir := filepath.Join(s.Settings().Storage, "backups")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return record, e
	}
	if e = noLinks(dir); e != nil {
		return record, e
	}
	target := filepath.Join(dir, "import-"+record.ID+".zip")
	f, e := os.CreateTemp(dir, ".import-*.partial")
	if e != nil {
		return record, e
	}
	partial := f.Name()
	defer os.Remove(partial)
	actual, copyErr := digestFile(path, f)
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || actual.SHA256 != before.SHA256 || actual.Size != before.Size {
		return record, fmt.Errorf("导入期间备份变化或复制失败，未加入备份库")
	}
	if e = moveNoReplace(partial, target); e != nil {
		return record, e
	}
	record.Path = target
	record.Bytes = actual.Size
	return record, nil
}
