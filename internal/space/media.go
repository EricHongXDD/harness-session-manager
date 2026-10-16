package space

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var attachmentID = regexp.MustCompile(`^sha256:([a-f0-9]{64})$`)
var attachmentPath = regexp.MustCompile(`^attachments/v1/objects/([a-f0-9]{2})/([a-f0-9]{64})$`)

type titleMemo struct {
	Identity string
	Size     int64
	Modified time.Time
	Latest   string
	First    string
}

func imagePath(id string) (string, error) {
	match := attachmentID.FindStringSubmatch(id)
	if match == nil {
		return "", fmt.Errorf("图片引用无效")
	}
	return "attachments/v1/objects/" + match[1][:2] + "/" + match[1], nil
}
func validAttachmentFile(file File) bool {
	m := attachmentPath.FindStringSubmatch(file.Path)
	return m != nil && m[1] == m[2][:2] && file.SHA256 == m[2]
}

// 只提取原生图片块的不可变附件引用，不使用正文中的路径访问本地文件。
func eventImages(data any) ([]ImageRef, error) {
	refs := []ImageRef{}
	message, ok := data.(map[string]any)
	if !ok {
		return refs, nil
	}
	if nested, ok := message["message"].(map[string]any); ok {
		message = nested
	}
	content, _ := message["content"].([]any)
	for _, value := range content {
		if v, ok := value.(map[string]any); ok {
			if v["type"] == "image" {
				a, ok := v["attachment"].(map[string]any)
				if !ok {
					return refs, fmt.Errorf("图片块缺少原生附件引用，无法保证图片备份完整")
				}
				id, _ := a["attachmentId"].(string)
				if _, e := imagePath(id); e != nil {
					return refs, e
				}
				name, _ := a["name"].(string)
				mime, _ := a["mediaType"].(string)
				refs = append(refs, ImageRef{ID: id, Name: name, MediaType: mime})
			}
		}
	}
	return refs, nil
}

func logImages(log string) ([]ImageRef, error) {
	refs := []ImageRef{}
	var issue error
	first := true
	e := scanLog(log, func(line []byte) bool {
		if first {
			first = false
			return true
		}
		var event struct {
			Data any `json:"data"`
		}
		if issue = json.Unmarshal(line, &event); issue != nil {
			return false
		}
		var found []ImageRef
		found, issue = eventImages(event.Data)
		refs = append(refs, found...)
		if len(refs) > maxFiles {
			issue = fmt.Errorf("图片引用数量超限")
		}
		return issue == nil
	})
	if e != nil {
		return refs, e
	}
	return refs, issue
}

func cleanTitle(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) > limit {
		value = string([]rune(value)[:limit]) + "…"
	}
	return value
}

// 标题事件是原生数据源；没有事件时兼容已有缓存，再回退到首条用户文本。
func (s *Service) conversationTitle(h Header, log, fallback string) string {
	latest, firstText := "", ""
	info, statErr := regular(log)
	identity, _, identityErr := nativeStat(log)
	s.mu.RLock()
	memo, found := s.titles[log]
	s.mu.RUnlock()
	if statErr == nil && identityErr == nil && found && memo.Identity == identity && memo.Size == info.Size() && memo.Modified.Equal(info.ModTime()) {
		if memo.Latest != "" {
			return memo.Latest
		}
		if cached := s.cachedTitle(h, ""); cached != "" {
			return cached
		}
		if memo.First != "" {
			return memo.First
		}
		return fallback
	}
	first := true
	err := scanLog(log, func(line []byte) bool {
		if first {
			first = false
			return true
		}
		var event struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(line, &event) != nil {
			return true
		}
		if event.Type == "session/title" {
			if v, ok := event.Data["title"].(string); ok && cleanTitle(v, 512) != "" {
				latest = cleanTitle(v, 512)
			}
		}
		if firstText == "" && event.Type == "user/message" {
			if source, ok := event.Data["source"].(map[string]any); ok && source["kind"] == "user" {
				firstText = cleanTitle(eventText(event.Data), 80)
			}
		}
		return true
	})
	if err == nil && statErr == nil && identityErr == nil {
		s.mu.Lock()
		if s.titles == nil || len(s.titles) > 4096 {
			s.titles = map[string]titleMemo{}
		}
		s.titles[log] = titleMemo{Identity: identity, Size: info.Size(), Modified: info.ModTime(), Latest: latest, First: firstText}
		s.mu.Unlock()
	}
	if err == nil && latest != "" {
		return latest
	}
	if cached := s.cachedTitle(h, ""); cached != "" {
		return cached
	}
	if latest != "" {
		return latest
	}
	if firstText != "" {
		return firstText
	}
	return fallback
}

func (s *Service) snapshotImages(conversations []Conversation) ([]File, error) {
	files := []File{}
	seen := map[string]bool{}
	for _, c := range conversations {
		if c.Log == "" || c.Warning != "" {
			return nil, fmt.Errorf("会话格式不明确，无法确认附件完整：%s", c.Title)
		}
		refs, e := logImages(c.Log)
		if e != nil {
			return nil, e
		}
		for _, ref := range refs {
			relative, e := imagePath(ref.ID)
			if e != nil {
				return nil, e
			}
			if seen[relative] {
				continue
			}
			seen[relative] = true
			file, e := digestFile(filepath.Join(s.Settings().Source, filepath.FromSlash(relative)), nil)
			if e != nil {
				return nil, fmt.Errorf("图片 %s 无法备份：%w", ref.ID, e)
			}
			file.Path = relative
			if !validAttachmentFile(file) {
				return nil, fmt.Errorf("图片内容与引用摘要不一致：%s", ref.ID)
			}
			files = append(files, file)
		}
	}
	return files, nil
}

// 图片读取必须由所选会话的引用授权，且只返回校验通过的本地栅格图片。
func (s *Service) Image(key, id string) (string, error) {
	catalog, e := s.List()
	if e != nil {
		return "", e
	}
	selected := ""
	for _, c := range catalog.Items {
		if c.Key == key && c.Warning == "" {
			selected = c.Log
			break
		}
	}
	if selected == "" {
		return "", fmt.Errorf("对话已不在当前来源")
	}
	refs, e := logImages(selected)
	if e != nil {
		return "", e
	}
	allowed := false
	for _, ref := range refs {
		if ref.ID == id {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("图片不属于所选对话")
	}
	relative, e := imagePath(id)
	if e != nil {
		return "", e
	}
	path := filepath.Join(s.Settings().Source, filepath.FromSlash(relative))
	info, e := regular(path)
	if e != nil {
		return "", fmt.Errorf("图片文件缺失或不可读取")
	}
	if info.Size() > 20<<20 {
		return "", fmt.Errorf("图片超过 20 MiB 预览限制，原文件仍会备份")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return "", fmt.Errorf("读取前图片已变化")
	}
	data, e := io.ReadAll(io.LimitReader(f, (20<<20)+1))
	if e != nil {
		return "", e
	}
	after, e := f.Stat()
	if e != nil || len(data) > 20<<20 || int64(len(data)) != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return "", fmt.Errorf("读取期间图片变化或超过预览限制")
	}
	digest := sha256.Sum256(data)
	if "sha256:"+hex.EncodeToString(digest[:]) != id {
		return "", fmt.Errorf("图片摘要校验失败")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", fmt.Errorf("不支持的图片格式，原文件仍会备份")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
