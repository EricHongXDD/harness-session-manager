package main

import (
	"context"
	"dsh-space/internal/space"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"log"
	"os"
	"path/filepath"
)

//go:embed all:frontend/dist
var assets embed.FS

type App struct {
	ctx     context.Context
	service *space.Service
	demo    bool
}

func newApp(demo bool) (*App, error) {
	if demo {
		return demoApp()
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	source := filepath.Join(home, ".dsh")
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local, _ = os.UserConfigDir()
	}
	storage := filepath.Join(local, "DSHSpace")
	settings := filepath.Join(storage, "settings.json")
	if data, e := os.ReadFile(settings); e == nil && len(data) < 1<<20 {
		var saved space.Settings
		if json.Unmarshal(data, &saved) == nil && saved.Source != "" {
			source = saved.Source
		}
	}
	service, e := space.New(source, storage, nil)
	return &App{service: service}, e
}
func (a *App) startup(ctx context.Context)           { a.ctx = ctx }
func (a *App) Settings() space.Settings              { cfg := a.service.Settings(); cfg.Demo = a.demo; return cfg }
func (a *App) Conversations() (space.Catalog, error) { return a.service.List() }
func (a *App) Read(key string, offset int) (space.MessagePage, error) {
	return a.service.Read(key, offset)
}
func (a *App) Image(key, id string) (string, error) { return a.service.Image(key, id) }
func (a *App) ImportBackup() (*space.Record, error) {
	if a.demo || a.ctx == nil {
		return nil, fmt.Errorf("请在正式桌面版选择 ZIP 备份导入")
	}
	chosen, e := wr.OpenFileDialog(a.ctx, wr.OpenDialogOptions{Title: "选择 Harness Session Manager ZIP 备份", Filters: []wr.FileFilter{{DisplayName: "Harness Session Manager / DSH Space 备份 (*.zip)", Pattern: "*.zip"}}})
	if e != nil || chosen == "" {
		return nil, e
	}
	record, e := a.service.ImportBackup(chosen)
	if e != nil {
		return nil, e
	}
	return &record, nil
}
func (a *App) Backup(keys []string) (space.Record, error) { return a.service.Backup(keys) }
func (a *App) PreviewHide(keys []string) (space.HidePreview, error) {
	return a.service.PreviewHide(keys)
}
func (a *App) Hide(request space.HideRequest) (space.Record, error) {
	return a.service.HideConfirmed(request)
}
func (a *App) HideDirect(keys []string) (space.Record, error) { return a.service.Hide(keys) }
func (a *App) PreviewDelete(request space.DeleteRequest) (space.DeletePreview, error) {
	return a.service.PreviewDelete(request)
}
func (a *App) DeleteConfirmed(token string) (space.DeleteResult, error) {
	return a.service.DeleteConfirmed(token)
}
func (a *App) Hidden() ([]space.Record, error)  { return a.service.Hidden() }
func (a *App) RestoreHidden(id string) error    { return a.service.RestoreHidden(id) }
func (a *App) Backups() ([]space.Record, error) { return a.service.Backups() }
func (a *App) RestoreBackup(id string) error    { return a.service.RestoreBackup(id) }
func (a *App) DeleteBackup(id string) error     { return a.service.DeleteBackup(id) }
func (a *App) ScanState() space.Usage           { return a.service.ScanState() }
func (a *App) StartScan() space.Usage           { return a.service.StartScan() }
func (a *App) CancelScan()                      { a.service.CancelScan() }
func (a *App) ChooseSource() (space.Settings, error) {
	if a.demo {
		return a.Settings(), fmt.Errorf("演示模式固定使用独立合成数据")
	}
	chosen, e := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "选择包含 sessions 文件夹的 .dsh 数据目录"})
	if e != nil || chosen == "" {
		return a.Settings(), e
	}
	if e = a.service.SetSource(chosen); e != nil {
		return a.Settings(), e
	}
	return a.Settings(), nil
}
func (a *App) OpenStorage() error {
	if a.ctx == nil {
		return fmt.Errorf("请在桌面版打开存储目录")
	}
	return openFolder(a.Settings().Storage)
}
func main() {
	preview := false
	demo := false
	for _, arg := range os.Args[1:] {
		if arg == "--demo" {
			demo = true
		}
		if arg == "--preview" {
			preview = true
			demo = true
		}
	}
	app, e := newApp(demo)
	if e != nil {
		log.Fatal(e)
	}
	defer app.service.Close()
	if preview {
		if e = servePreview(app); e != nil {
			log.Fatal(e)
		}
		return
	}
	instanceID := "dsh-space-v1"
	if app.demo {
		instanceID = "dsh-space-demo-v1"
	}
	e = wails.Run(&options.App{Title: "Harness Session Manager", Width: 1220, Height: 820, MinWidth: 960, MinHeight: 660, BackgroundColour: options.NewRGB(245, 247, 250), AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, Bind: []interface{}{app}, SingleInstanceLock: &options.SingleInstanceLock{UniqueId: instanceID, OnSecondInstanceLaunch: func(options.SecondInstanceData) { wr.WindowShow(app.ctx) }}, DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true}, DisableExternalNavigation: true})
	if e != nil {
		log.Fatal(e)
	}
}
