package space

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendSynthetic(t *testing.T, dir string, events ...map[string]any) {
	t.Helper()
	f, e := os.OpenFile(filepath.Join(dir, "session.v4.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	for _, event := range events {
		if e = json.NewEncoder(f).Encode(event); e != nil {
			t.Fatal(e)
		}
	}
}
func syntheticImage(t *testing.T, s *Service) (ImageRef, []byte, string) {
	t.Helper()
	var buf bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{10, 20, 30, 255})
	if e := png.Encode(&buf, canvas); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(buf.Bytes())
	ref := ImageRef{ID: "sha256:" + hex.EncodeToString(h[:]), Name: "中文图片.png", MediaType: "image/png"}
	rel, _ := imagePath(ref.ID)
	path := filepath.Join(s.Settings().Source, filepath.FromSlash(rel))
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, buf.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	return ref, buf.Bytes(), path
}
func attachSynthetic(t *testing.T, dir string, ref ImageRef) {
	t.Helper()
	appendSynthetic(t, dir, map[string]any{"seq": 6, "time": 1792000000006, "type": "user/message", "surfaceOp": "append", "data": map[string]any{"id": "user-2", "role": "user", "source": map[string]any{"kind": "user"}, "content": []any{map[string]any{"type": "text", "text": "看看这张图片"}, map[string]any{"type": "image", "attachment": map[string]any{"attachmentId": ref.ID, "mediaType": ref.MediaType, "name": ref.Name, "width": 2, "height": 2, "bytes": 80}}}}})
}
func renameSynthetic(t *testing.T, dir, title string) {
	t.Helper()
	appendSynthetic(t, dir, map[string]any{"seq": 7, "time": 1792000000007, "type": "session/title", "data": map[string]any{"title": title, "source": map[string]any{"kind": "user"}, "messageSeqs": []any{}}})
}

func TestLatestLoggedTitleAndFallbackWithoutProjectionCache(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-title", "", false)
	renameSynthetic(t, dir, "用户备注 🐳")
	if got := catalog(t, s).Items[0].Title; got != "用户备注 🐳" {
		t.Fatal(got)
	}
	renameSynthetic(t, dir, "最终备注")
	if got := catalog(t, s).Items[0].Title; got != "最终备注" {
		t.Fatal(got)
	}
	s2 := fixture(t)
	session(t, s2, "session-fallback", "", false)
	path := filepath.Join(s2.Settings().Source, "storages", "session_projcache", "sessions", "session-fallback.json")
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if got := catalog(t, s2).Items[0].Title; got != "完整中文与 Emoji 🐳" {
		t.Fatal(got)
	}
}

func TestImageReadBackupImportAndPortableRestore(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-picture", "", false)
	ref, original, imageFile := syntheticImage(t, s)
	attachSynthetic(t, dir, ref)
	renameSynthetic(t, dir, "带图片的备注")
	c := catalog(t, s).Items[0]
	page, e := s.Read(c.Key, 0)
	if e != nil || len(page.Items[6].Images) != 1 || page.Items[6].Images[0].ID != ref.ID {
		t.Fatal(page, e)
	}
	src, e := s.Image(c.Key, ref.ID)
	if e != nil || !strings.HasPrefix(src, "data:image/png;base64,") {
		t.Fatal(e)
	}
	if _, e = s.Image(c.Key, "sha256:"+strings.Repeat("0", 64)); e == nil {
		t.Fatal("读取了未引用图片")
	}
	backup, e := s.Backup([]string{c.Key})
	if e != nil {
		t.Fatal(e)
	}
	if backup.FormatVersion != 2 || len(backup.Attachments) != 1 {
		t.Fatal(backup)
	}
	external := filepath.Join(t.TempDir(), "中文备份.zip")
	zipBytes, e := os.ReadFile(backup.Path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(external, zipBytes, 0600); e != nil {
		t.Fatal(e)
	}
	target := fixture(t)
	imported, e := target.ImportBackup(external)
	if e != nil {
		t.Fatal(e)
	}
	if imported.Path == external || len(catalog(t, target).Items) != 0 {
		t.Fatal("导入改写了来源")
	}
	if duplicate, e := target.ImportBackup(external); e != nil || duplicate.ID != imported.ID {
		t.Fatal(duplicate, e)
	}
	if e = target.RestoreBackup(imported.ID); e != nil {
		t.Fatal(e)
	}
	restored := catalog(t, target).Items[0]
	if restored.Title != "带图片的备注" {
		t.Fatal(restored.Title)
	}
	rel, _ := imagePath(ref.ID)
	data, e := os.ReadFile(filepath.Join(target.Settings().Source, filepath.FromSlash(rel)))
	if e != nil || !bytes.Equal(data, original) {
		t.Fatal(e)
	}
	if _, e = target.Image(restored.Key, ref.ID); e != nil {
		t.Fatal(e)
	}
	if e = target.RestoreBackup(imported.ID); e != nil {
		t.Fatal("重复恢复失败", e)
	}
	// 图片损坏时停止备份，不生成只有会话的伪完整副本。
	if e = os.WriteFile(imageFile, []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Image(c.Key, ref.ID); e == nil {
		t.Fatal("损坏图片通过预览")
	}
	if _, e = s.Backup([]string{c.Key}); e == nil {
		t.Fatal("损坏图片通过备份")
	}
	if e = os.Remove(imageFile); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Backup([]string{c.Key}); e == nil {
		t.Fatal("缺失图片通过备份")
	}
	records, e := s.Backups()
	if e != nil || len(records) != 1 {
		t.Fatal(records, e)
	}
}

func TestImportRejectsTamperedImageAndMissingManifestReference(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-picture", "", false)
	ref, _, _ := syntheticImage(t, s)
	attachSynthetic(t, dir, ref)
	backup, e := s.Backup([]string{catalog(t, s).Items[0].Key})
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"tamper", "omit-reference", "duplicate-manifest"} {
		t.Run(mode, func(t *testing.T) {
			input, e := zip.OpenReader(backup.Path)
			if e != nil {
				t.Fatal(e)
			}
			defer input.Close()
			path := filepath.Join(t.TempDir(), "bad.zip")
			f, e := os.Create(path)
			if e != nil {
				t.Fatal(e)
			}
			out := zip.NewWriter(f)
			for _, entry := range input.File {
				reader, e := entry.Open()
				if e != nil {
					t.Fatal(e)
				}
				data, e := io.ReadAll(reader)
				reader.Close()
				if e != nil {
					t.Fatal(e)
				}
				if strings.HasPrefix(entry.Name, "attachments/") {
					if mode == "tamper" {
						data[0] ^= 1
					}
					if mode == "omit-reference" {
						continue
					}
				}
				if entry.Name == "dsh-space-manifest.json" && mode == "omit-reference" {
					var record Record
					json.Unmarshal(data, &record)
					record.Attachments = nil
					data, _ = json.Marshal(record)
				}
				w, e := out.Create(entry.Name)
				if e != nil {
					t.Fatal(e)
				}
				w.Write(data)
				if mode == "duplicate-manifest" && entry.Name == "dsh-space-manifest.json" {
					w, _ = out.Create(entry.Name)
					w.Write(data)
				}
			}
			if e = out.Close(); e != nil {
				t.Fatal(e)
			}
			f.Close()
			target := fixture(t)
			if _, e = target.ImportBackup(path); e == nil {
				t.Fatal("损坏或缺图 ZIP 被导入")
			}
			records, _ := target.Backups()
			if len(records) != 0 || len(catalog(t, target).Items) != 0 {
				t.Fatal("失败导入产生可用记录")
			}
			dirs, e := os.ReadDir(filepath.Join(target.Settings().Storage, "staging"))
			if e != nil || len(dirs) != 0 {
				t.Fatal("失败校验未清理暂存", dirs, e)
			}
		})
	}
}

func TestImageConflictStopsBeforeSessionRestore(t *testing.T) {
	s := fixture(t)
	dir := session(t, s, "session-picture", "", false)
	ref, _, _ := syntheticImage(t, s)
	attachSynthetic(t, dir, ref)
	backup, e := s.Backup([]string{catalog(t, s).Items[0].Key})
	if e != nil {
		t.Fatal(e)
	}
	target := fixture(t)
	imported, e := target.ImportBackup(backup.Path)
	if e != nil {
		t.Fatal(e)
	}
	rel, _ := imagePath(ref.ID)
	path := filepath.Join(target.Settings().Source, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("不同图片"), 0600)
	if e = target.RestoreBackup(imported.ID); e == nil {
		t.Fatal("覆盖了不同图片")
	}
	if len(catalog(t, target).Items) != 0 {
		t.Fatal("图片冲突时写入了会话")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "不同图片" {
		t.Fatal("原图片被覆盖")
	}
}

func TestSharedImagesDeduplicatedAndNonMessageMetadataIgnored(t *testing.T) {
	s := fixture(t)
	ref, _, _ := syntheticImage(t, s)
	keys := []string{}
	for _, id := range []string{"session-first", "session-second"} {
		dir := session(t, s, id, "", false)
		attachSynthetic(t, dir, ref)
		appendSynthetic(t, dir, map[string]any{"seq": 8, "type": "request/header", "data": map[string]any{"tools": []any{map[string]any{"schema": map[string]any{"type": "image"}}}}})
	}
	for _, c := range catalog(t, s).Items {
		keys = append(keys, c.Key)
	}
	record, e := s.Backup(keys)
	if e != nil || len(record.Members) != 2 || len(record.Attachments) != 1 {
		t.Fatal(record, e)
	}
}

func TestJPEGAndGIFPreviewUsesActualFileType(t *testing.T) {
	for _, mime := range []string{"image/jpeg", "image/gif"} {
		t.Run(mime, func(t *testing.T) {
			s := fixture(t)
			dir := session(t, s, "session-raster", "", false)
			canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
			canvas.Set(1, 1, color.RGBA{20, 30, 40, 255})
			var buf bytes.Buffer
			var e error
			if mime == "image/jpeg" {
				e = jpeg.Encode(&buf, canvas, nil)
			} else {
				e = gif.Encode(&buf, canvas, nil)
			}
			if e != nil {
				t.Fatal(e)
			}
			digest := sha256.Sum256(buf.Bytes())
			ref := ImageRef{ID: "sha256:" + hex.EncodeToString(digest[:]), MediaType: mime}
			rel, _ := imagePath(ref.ID)
			path := filepath.Join(s.Settings().Source, filepath.FromSlash(rel))
			os.MkdirAll(filepath.Dir(path), 0700)
			os.WriteFile(path, buf.Bytes(), 0600)
			attachSynthetic(t, dir, ref)
			src, e := s.Image(catalog(t, s).Items[0].Key, ref.ID)
			if e != nil || !strings.HasPrefix(src, "data:"+mime+";base64,") {
				t.Fatal(src, e)
			}
		})
	}
}

func TestLegacyBackupImportsAndRestoresWithoutFormatUpgrade(t *testing.T) {
	s := fixture(t)
	session(t, s, "session-legacy", "", false)
	record, e := s.Backup([]string{catalog(t, s).Items[0].Key})
	if e != nil {
		t.Fatal(e)
	}
	archive, e := zip.OpenReader(record.Path)
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	path := filepath.Join(t.TempDir(), "old.zip")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	out := zip.NewWriter(f)
	for _, entry := range archive.File {
		r, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if entry.Name == "dsh-space-manifest.json" {
			record.FormatVersion = 0
			data, _ = json.Marshal(record)
		}
		w, e := out.Create(entry.Name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write(data)
	}
	if e = out.Close(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	target := fixture(t)
	imported, e := target.ImportBackup(path)
	if e != nil || imported.FormatVersion != 0 {
		t.Fatal(imported, e)
	}
	if e = target.RestoreBackup(imported.ID); e != nil {
		t.Fatal(e)
	}
	if len(catalog(t, target).Items) != 1 {
		t.Fatal("旧版恢复遗漏")
	}
}
