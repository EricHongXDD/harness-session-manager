package space

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Service struct {
	mu          sync.RWMutex
	operation   sync.Mutex
	config      Settings
	probe       func() error
	usage       Usage
	cancel      context.CancelFunc
	scanDone    chan struct{}
	scanEpoch   uint64
	titles      map[string]titleMemo
	deletePlans map[string]deletePlan
}

func New(source, storage string, probe func() error) (*Service, error) {
	source, e := filepath.Abs(source)
	if e != nil {
		return nil, e
	}
	storage, e = filepath.Abs(storage)
	if e != nil {
		return nil, e
	}
	if within(source, storage) || within(storage, source) {
		return nil, fmt.Errorf("应用存储目录必须在 Harness 数据目录以外")
	}
	if e = os.MkdirAll(storage, 0700); e != nil {
		return nil, e
	}
	if e = noLinks(storage); e != nil {
		return nil, e
	}
	if probe == nil {
		probe = CheckHarnessStopped
	}
	s := &Service{config: Settings{Source: source, Storage: storage}, probe: probe}
	s.usage = Usage{Root: source, Categories: []Category{}}
	return s, nil
}
func (s *Service) Settings() Settings { s.mu.RLock(); defer s.mu.RUnlock(); return s.config }
func (s *Service) SetSource(source string) error {
	if !s.operation.TryLock() {
		return fmt.Errorf("有操作正在执行，请稍后切换目录")
	}
	defer s.operation.Unlock()
	source, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	cfg := s.Settings()
	if within(source, cfg.Storage) || within(cfg.Storage, source) {
		return fmt.Errorf("来源与应用存储不能重叠")
	}
	sessions := filepath.Join(source, "sessions")
	if e = noLinks(sessions); e != nil {
		return fmt.Errorf("请选择包含 sessions 的 Harness 数据目录")
	}
	info, e := os.Stat(sessions)
	if e != nil || !info.IsDir() {
		return fmt.Errorf("sessions 必须是普通目录")
	}
	// 先保存成功，再切换内存来源；保存失败不能让界面与实际读取目录分离。
	cfg.Source = source
	data, e := json.MarshalIndent(cfg, "", "  ")
	if e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(cfg.Storage, "settings.json"), data); e != nil {
		return e
	}
	s.stopScan()
	s.mu.Lock()
	s.scanEpoch++
	s.config.Source = source
	s.usage = Usage{Root: source, Categories: []Category{}}
	s.mu.Unlock()
	return nil
}
func (s *Service) Close() { s.stopScan() }
func (s *Service) stopScan() {
	s.mu.RLock()
	done, cancel := s.scanDone, s.cancel
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
func (s *Service) guard() error {
	if s.probe == nil {
		return fmt.Errorf("无法确认 Harness 退出状态")
	}
	if e := s.probe(); e != nil {
		return e
	}
	return noLinks(filepath.Join(s.Settings().Source, "sessions"))
}
func (s *Service) lock() error {
	if !s.operation.TryLock() {
		return fmt.Errorf("有备份、隐藏或恢复正在执行，请稍后")
	}
	s.stopScan()
	return nil
}
