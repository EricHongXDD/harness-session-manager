package space

import (
	"fmt"
	"path/filepath"
)

type HideRequest struct {
	Keys         []string `json:"keys"`
	ExpectedKeys []string `json:"expectedKeys"`
	Source       string   `json:"source"`
	Identity     string   `json:"identity"`
}
type HidePreview struct {
	Items       []Conversation `json:"items"`
	Source      string         `json:"source"`
	Identity    string         `json:"identity"`
	Destination string         `json:"destination"`
}

func (s *Service) PreviewHide(keys []string) (HidePreview, error) {
	if e := s.lock(); e != nil {
		return HidePreview{}, e
	}
	defer s.operation.Unlock()
	if e := s.guard(); e != nil {
		return HidePreview{}, e
	}
	cfg := s.Settings()
	items, e := s.selection(keys, true)
	if e != nil {
		return HidePreview{}, e
	}
	id, _, e := nativeStat(filepath.Join(cfg.Source, "sessions"))
	if e != nil {
		return HidePreview{}, fmt.Errorf("来源身份不可取得")
	}
	return HidePreview{Items: items, Source: cfg.Source, Identity: id, Destination: filepath.Join(cfg.Storage, "hidden")}, nil
}
