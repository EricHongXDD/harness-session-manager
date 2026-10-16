package space

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Service) ScanState() Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.usage
	out.Categories = append([]Category{}, out.Categories...)
	return out
}
func (s *Service) CancelScan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) StartScan() Usage {
	// 来源切换和移动目录期间不能启动新扫描，避免跨范围混计。
	if !s.operation.TryLock() {
		return s.ScanState()
	}
	defer s.operation.Unlock()
	cfg := s.Settings()
	s.mu.Lock()
	if s.usage.Running {
		out := s.usage
		s.mu.Unlock()
		return out
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.scanEpoch++
	epoch := s.scanEpoch
	done := make(chan struct{})
	s.scanDone = done
	s.usage = Usage{Running: true, Root: cfg.Source, Categories: []Category{}}
	out := s.usage
	s.mu.Unlock()
	go s.scan(ctx, cfg, epoch, done)
	return out
}
func (s *Service) scan(ctx context.Context, cfg Settings, epoch uint64, done chan struct{}) {
	defer close(done)
	result := Usage{Running: true, Root: cfg.Source, Categories: []Category{}}
	categories := map[string]*Category{}
	seen := map[string]bool{}
	physical := int64(0)
	unknown := false
	publish := func(final bool) {
		result.Categories = []Category{}
		keys := []string{}
		for name := range categories {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			category := *categories[name]
			if category.Allocated != nil {
				value := *category.Allocated
				category.Allocated = &value
			}
			if unknown || result.Errors > 0 || result.Canceled {
				category.Allocated = nil
			}
			result.Categories = append(result.Categories, category)
		}
		if final {
			result.Running = false
			result.Complete = !result.Canceled && result.Errors == 0
			result.At = time.Now().UTC()
		}
		result.Allocated = nil
		if !unknown && result.Complete {
			allocated := physical
			result.Allocated = &allocated
		}
		s.mu.Lock()
		if s.config.Source == cfg.Source && s.scanEpoch == epoch {
			s.usage = result
		}
		s.mu.Unlock()
	}
	for _, root := range []string{cfg.Source, cfg.Storage} {
		if ctx.Err() != nil {
			break
		}
		if e := noLinks(root); e != nil {
			result.Errors++
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				result.Errors++
				return nil
			}
			i, e := os.Lstat(path)
			if e != nil {
				result.Errors++
				return nil
			}
			if i.Mode()&os.ModeSymlink != 0 || reparse(i) {
				result.Errors++
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !i.Mode().IsRegular() {
				result.Errors++
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			first := strings.Split(relative, string(filepath.Separator))[0]
			name := "Harness 其它数据"
			if root == cfg.Storage {
				name = "DSH Space 设置"
				if first == "hidden" {
					name = "已隐藏对话"
				} else if first == "backups" {
					name = "本地备份"
				}
			} else {
				switch first {
				case "sessions":
					name = "活跃会话"
				case "dsh-runtimes":
					name = "Harness 运行时"
				case "storages":
					name = "Harness 索引与缓存"
				case "profiles":
					name = "Harness 配置"
				}
			}
			category := categories[name]
			if category == nil {
				zero := int64(0)
				category = &Category{Name: name, Allocated: &zero}
				categories[name] = category
			}
			category.Files++
			category.Bytes += i.Size()
			result.Files++
			result.Bytes += i.Size()
			identity, allocated, e := nativeStat(path)
			if e != nil || identity == "" {
				unknown = true
			} else if !seen[identity] {
				seen[identity] = true
				physical += allocated
				if category.Allocated != nil {
					*category.Allocated += allocated
				}
			}
			if result.Files%100 == 0 {
				publish(false)
			}
			return nil
		})
	}
	result.Canceled = ctx.Err() != nil
	publish(true)
}
