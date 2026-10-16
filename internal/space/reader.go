package space

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var generation = regexp.MustCompile(`^session\.v([1-9][0-9]*)\.jsonl(\.zstd)?$`)

const maxLine = 8 << 20

func encodedID(id string) string {
	var b strings.Builder
	for _, u := range utf16.Encode([]rune(id)) {
		c := rune(u)
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c) {
			b.WriteRune(c)
		} else {
			fmt.Fprintf(&b, "~%04X", u)
		}
	}
	if id == "." {
		return "~002E"
	}
	if id == ".." {
		return "~002E~002E"
	}
	return b.String()
}
func selectedLog(dir string) (string, int, error) {
	entries, e := os.ReadDir(dir)
	if e != nil {
		return "", 0, e
	}
	version := -1
	selected := ""
	ambiguous := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		v := -1
		if name == "session.jsonl" || name == "session.jsonl.zstd" {
			v = 0
		} else if m := generation.FindStringSubmatch(name); m != nil {
			v, _ = strconv.Atoi(m[1])
		}
		if v > version {
			version = v
			selected = filepath.Join(dir, name)
			ambiguous = false
		} else if v >= 0 && v == version {
			ambiguous = true
		}
	}
	if version < 0 {
		return "", 0, fmt.Errorf("没有已识别的会话文件")
	}
	if ambiguous {
		return "", version, fmt.Errorf("同一代有多个会话载体，不能猜测")
	}
	return selected, version, nil
}
func scanLog(path string, visit func([]byte) bool) error {
	before, e := regular(path)
	if e != nil {
		return e
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		return fmt.Errorf("会话文件已变化")
	}
	var reader io.Reader = f
	if strings.HasSuffix(path, ".zstd") {
		decoder, e := zstd.NewReader(f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20), zstd.WithDecoderMaxWindow(32<<20))
		if e != nil {
			return e
		}
		defer decoder.Close()
		reader = decoder
	}
	limited := &io.LimitedReader{R: reader, N: (512 << 20) + 1}
	stopped := false
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	for scanner.Scan() {
		if !visit(scanner.Bytes()) {
			stopped = true
			break
		}
	}
	if e = scanner.Err(); e != nil {
		return fmt.Errorf("读取不完整：%w", e)
	}
	if !stopped && limited.N == 0 {
		return fmt.Errorf("读取超过 512 MiB 安全预算，请查看完整备份")
	}
	after, e := f.Stat()
	if e != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return fmt.Errorf("读取期间会话发生变化，请刷新")
	}
	return nil
}
func readHeader(path string, version int) (Header, error) {
	var h Header
	var issue error
	var fields map[string]json.RawMessage
	e := scanLog(path, func(line []byte) bool {
		issue = json.Unmarshal(line, &h)
		if issue == nil {
			issue = json.Unmarshal(line, &fields)
		}
		return false
	})
	if e != nil {
		return h, e
	}
	if issue != nil {
		return h, fmt.Errorf("会话头损坏")
	}
	for _, key := range []string{"type", "version", "id", "createdAt", "isSeeded", "delegationDepth"} {
		raw, ok := fields[key]
		if !ok || string(raw) == "null" {
			return h, fmt.Errorf("会话头缺少必填字段 %s", key)
		}
	}
	allowed := map[string]bool{}
	for _, key := range []string{"type", "version", "id", "createdAt", "isSeeded", "delegationDepth", "cwd", "parentSession", "origin", "agentPreset"} {
		allowed[key] = true
	}
	for key, raw := range fields {
		if !allowed[key] || string(raw) == "null" {
			return h, fmt.Errorf("会话头含未支持字段 %s", key)
		}
	}
	if h.Origin != "" && h.Origin != "subagent" {
		return h, fmt.Errorf("会话来源标记无效")
	}
	if version != 4 || h.Version != version || h.Type != "session" || h.ID == "" || h.Created < 0 || h.Created > 9007199254740991 || h.Depth < 0 {
		return h, fmt.Errorf("未支持的会话格式 v%d；可备份原始目录", version)
	}
	return h, nil
}
func (s *Service) List() (Catalog, error) {
	cfg := s.Settings()
	result := Catalog{Items: []Conversation{}, Warnings: []string{}, Source: cfg.Source}
	root := filepath.Join(cfg.Source, "sessions")
	if e := noLinks(root); e != nil {
		return result, fmt.Errorf("会话目录不可读取：%w", e)
	}
	projects, e := os.ReadDir(root)
	if e != nil {
		return result, e
	}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, project.Name())
		if e = noLinks(projectDir); e != nil {
			result.Warnings = append(result.Warnings, "项目目录链接已跳过")
			continue
		}
		dirs, e := os.ReadDir(projectDir)
		if e != nil {
			result.Warnings = append(result.Warnings, "部分项目目录无法枚举")
			continue
		}
		for _, entry := range dirs {
			if !entry.IsDir() {
				if strings.HasSuffix(entry.Name(), ".jsonl") || strings.HasSuffix(entry.Name(), ".jsonl.zstd") {
					result.Warnings = append(result.Warnings, "发现未支持的旧式平铺会话载体")
				}
				continue
			}
			dir := filepath.Join(projectDir, entry.Name())
			relative := filepath.ToSlash(filepath.Join(project.Name(), entry.Name()))
			key := sha256.Sum256([]byte(relative))
			row := Conversation{Key: hex.EncodeToString(key[:]), Relative: relative, Path: dir, Title: entry.Name()}
			if e = noLinks(dir); e != nil {
				row.Warning = e.Error()
				result.Items = append(result.Items, row)
				continue
			}
			row.Log, row.Version, e = selectedLog(dir)
			if e != nil {
				row.Warning = e.Error()
			} else {
				h, he := readHeader(row.Log, row.Version)
				row.ID = h.ID
				row.CWD = h.CWD
				row.Parent = h.Parent
				row.Created = h.Created
				if he != nil {
					row.Warning = he.Error()
				} else if encodedID(h.ID) != entry.Name() || ProjectDirectory(h.CWD) != project.Name() {
					row.Warning = "会话 ID 或工作目录与存储路径不一致，不能移动"
				}
				if info, ie := os.Stat(row.Log); ie == nil {
					row.Modified = info.ModTime()
				}
				row.Title = s.conversationTitle(h, row.Log, entry.Name())
			}
			filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					row.Warning = "部分文件无法计量"
					return nil
				}
				i, ie := os.Lstat(p)
				if ie != nil {
					row.Warning = "部分文件无法计量"
					return nil
				}
				if i.Mode()&os.ModeSymlink != 0 || reparse(i) {
					row.Warning = "会话包含链接，不能备份或隐藏"
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if i.Mode().IsRegular() {
					row.Bytes += i.Size()
					row.Files++
				}
				return nil
			})
			result.Items = append(result.Items, row)
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Modified.After(result.Items[j].Modified) })
	s.attachWorkspaces(&result)
	return result, nil
}
func (s *Service) cachedTitle(header Header, fallback string) string {
	if header.ID == "" || len(header.ID) > 512 {
		return fallback
	}
	path := filepath.Join(s.Settings().Source, "storages", "session_projcache", "sessions", encodedID(header.ID)+".json")
	info, e := regular(path)
	if e != nil || info.Size() > 2<<20 {
		return fallback
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return fallback
	}
	// 兼容原生 per-record v3 至 v7 的 title v1 提示，并核对会话生命周期。
	var cached struct {
		Version int `json:"version"`
		Record  struct {
			Identity struct {
				Version int    `json:"formatVersion"`
				Created int64  `json:"createdAt"`
				CWD     string `json:"cwd"`
				Seeded  bool   `json:"isSeeded"`
			} `json:"identity"`
			Rows map[string]struct {
				Version int     `json:"ver"`
				Value   *string `json:"val"`
			} `json:"rows"`
		} `json:"record"`
	}
	if json.Unmarshal(data, &cached) != nil || cached.Version < 3 || cached.Version > 7 || cached.Record.Identity.Version > header.Version || cached.Record.Identity.Created != header.Created || cached.Record.Identity.CWD != header.CWD || cached.Record.Identity.Seeded != header.Seeded {
		return fallback
	}
	title, ok := cached.Record.Rows["title"]
	if !ok || title.Version != 1 || title.Value == nil {
		return fallback
	}
	value := strings.TrimSpace(*title.Value)
	if value == "" || len(value) > 4096 {
		return fallback
	}
	return value
}

func (s *Service) Read(key string, offset int) (MessagePage, error) {
	page := MessagePage{Items: []Message{}, Next: offset}
	if offset < 0 || offset > 1000000 {
		return page, fmt.Errorf("消息游标无效")
	}
	catalog, e := s.List()
	if e != nil {
		return page, e
	}
	var selected *Conversation
	for i := range catalog.Items {
		if catalog.Items[i].Key == key {
			selected = &catalog.Items[i]
			break
		}
	}
	if selected == nil {
		return page, fmt.Errorf("对话已不在当前来源")
	}
	if selected.Warning != "" {
		return page, fmt.Errorf("%s", selected.Warning)
	}
	index := 0
	size := 0
	first := true
	e = scanLog(selected.Log, func(line []byte) bool {
		if first {
			first = false
			return true
		}
		var event struct {
			Seq  int64           `json:"seq"`
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(line, &event) != nil {
			page.Warning = "部分记录无法解析"
			return false
		}
		if index < offset {
			index++
			return true
		}
		if len(page.Items) >= 100 || len(page.Items) > 0 && size+len(line) > 1<<20 {
			page.HasMore = true
			return false
		}
		var data map[string]any
		_ = json.Unmarshal(event.Data, &data)
		role := "事件"
		switch {
		case event.Type == "user/message":
			role = "你"
		case strings.HasPrefix(event.Type, "assistant/"):
			role = "助手"
		case strings.HasPrefix(event.Type, "tool/"):
			role = "工具"
		}
		text := eventText(data)
		if text == "" {
			text = string(event.Data)
		}
		if len(text) > 64<<10 {
			end := 64 << 10
			for end > 0 && !utf8.RuneStart(text[end]) {
				end--
			}
			text = text[:end] + "\n…此事件过长，请查看原始备份"
		}
		images, imageErr := eventImages(data)
		if imageErr != nil {
			page.Warning = imageErr.Error()
		}
		if len(images) > 0 && text == string(event.Data) {
			text = ""
		}
		page.Items = append(page.Items, Message{Seq: event.Seq, Type: event.Type, Role: role, Text: text, Images: images})
		size += len(line)
		index++
		page.Next = index
		return true
	})
	return page, e
}
func eventText(data map[string]any) string {
	// 助手、系统及工具结果的原生正文放在 data.message；用户消息直接存于 data。
	if message, ok := data["message"].(map[string]any); ok {
		return eventText(message)
	}
	if text, ok := data["text"].(string); ok {
		return text
	}
	if title, ok := data["title"].(string); ok {
		return title
	}
	var pieces []string
	if content, ok := data["content"].([]any); ok {
		for _, block := range content {
			if item, ok := block.(map[string]any); ok {
				if text, ok := item["text"].(string); ok {
					pieces = append(pieces, text)
				}
			}
		}
	}
	if len(pieces) > 0 {
		return strings.Join(pieces, "\n")
	}
	return ""
}

func conversationKey(relative string) string {
	h := sha256.Sum256([]byte(relative))
	return hex.EncodeToString(h[:])
}

// ProjectDirectory复现已核验版本的项目路径编码，不将该显示元数据作为目录访问授权。
func ProjectDirectory(cwd string) string {
	if cwd == "" {
		return "_no-cwd"
	}
	var b strings.Builder
	separator := false
	for _, u := range utf16.Encode([]rune(cwd)) {
		c := rune(u)
		if c == '/' || c == '\\' || c == ':' {
			if !separator {
				b.WriteByte('-')
			}
			separator = true
		} else if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c) {
			b.WriteRune(c)
			separator = false
		} else {
			fmt.Fprintf(&b, "~%04X", u)
			separator = false
		}
	}
	text := strings.TrimLeft(b.String(), "-")
	if text == "" {
		text = "root"
	}
	if len(text) > 251 {
		text = text[:251]
	}
	return "--" + text + "--"
}
