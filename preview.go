package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"dsh-space/internal/space"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// 演示与浏览器验收只创建进程自有临时来源，不接受外部数据路径或切换来源。
func demoApp() (*App, error) {
	base, e := os.MkdirTemp("", "dsh-space-demo-")
	if e != nil {
		return nil, e
	}
	return demoAppAt(base)
}
func demoAppAt(base string) (*App, error) {
	var e error
	source := filepath.Join(base, "source")
	storage := filepath.Join(base, "app")
	var pixels bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			canvas.Set(x, y, color.RGBA{uint8(x * 5), uint8(y * 7), 180, 255})
		}
	}
	if e = png.Encode(&pixels, canvas); e != nil {
		return nil, e
	}
	digest := sha256.Sum256(pixels.Bytes())
	hexDigest := hex.EncodeToString(digest[:])
	imagePath := filepath.Join(source, "attachments", "v1", "objects", hexDigest[:2], hexDigest)
	if e = os.MkdirAll(filepath.Dir(imagePath), 0700); e != nil {
		return nil, e
	}
	if e = os.WriteFile(imagePath, pixels.Bytes(), 0600); e != nil {
		return nil, e
	}
	for i, title := range []string{"规划一个更轻巧的本地工具", "排查服务启动慢的问题", "整理本周的项目进展"} {
		id := fmt.Sprintf("session-demo-%d", i)
		cwd := `C:\演示项目\DSH Space`
		if i == 2 {
			cwd = `C:\演示项目\default-workspace`
		}
		dir := filepath.Join(source, "sessions", space.ProjectDirectory(cwd), id)
		if e = os.MkdirAll(dir, 0700); e != nil {
			return nil, e
		}
		header := space.Header{Type: "session", Version: 4, ID: id, CWD: cwd, Created: time.Now().Add(time.Duration(-i) * 24 * time.Hour).UnixMilli()}
		data, _ := json.Marshal(header)
		data = append(data, '\n')
		events := []map[string]any{
			{"seq": 0, "time": header.Created, "type": "user/message", "surfaceOp": "append", "data": map[string]any{"id": "user-1", "role": "user", "source": map[string]any{"kind": "user"}, "content": []any{map[string]any{"type": "text", "text": title}}}},
			{"seq": 1, "time": header.Created + 1, "type": "turn/start", "data": map[string]any{"turn": 1}},
			{"seq": 2, "time": header.Created + 2, "type": "step/start", "data": map[string]any{"turn": 1, "step": 1}},
			{"seq": 3, "time": header.Created + 3, "type": "assistant/message", "surfaceOp": "append", "data": map[string]any{"turn": 1, "step": 1, "stream": []any{}, "message": map[string]any{"id": "assistant-1", "role": "assistant", "source": map[string]any{"kind": "model", "provider": "demo", "model": "synthetic"}, "content": []any{map[string]any{"type": "text", "text": "这是独立合成演示数据。可以在这里测试备份、隐藏和恢复，真实 Harness 对话不会被访问。"}}}}},
			{"seq": 4, "time": header.Created + 4, "type": "step/end", "data": map[string]any{"turn": 1, "step": 1}},
			{"seq": 5, "time": header.Created + 5, "type": "turn/end", "data": map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}},
		}
		if i == 0 {
			data := events[0]["data"].(map[string]any)
			data["content"] = append(data["content"].([]any), map[string]any{"type": "image", "attachment": map[string]any{"attachmentId": "sha256:" + hexDigest, "mediaType": "image/png", "width": 48, "height": 32, "bytes": pixels.Len(), "name": "合成图片.png"}})
		}
		events = append(events, map[string]any{"seq": 6, "time": header.Created + 6, "type": "session/title", "data": map[string]any{"title": title, "messageSeqs": []int{}, "source": map[string]any{"kind": "user"}}})
		for _, event := range events {
			line, _ := json.Marshal(event)
			data = append(data, append(line, '\n')...)
		}
		enc, _ := zstd.NewWriter(nil)
		// 原生 Zstandard 文件要求文件头独占首帧，事件从后续帧开始。
		split := bytes.IndexByte(data, '\n') + 1
		encoded := enc.EncodeAll(data[:split], nil)
		encoded = append(encoded, enc.EncodeAll(data[split:], nil)...)
		enc.Close()
		os.WriteFile(filepath.Join(dir, "session.v4.jsonl.zstd"), encoded, 0600)
		cache := map[string]any{"version": 7, "record": map[string]any{"identity": map[string]any{"formatVersion": 4, "createdAt": header.Created, "cwd": header.CWD}, "rows": map[string]any{"title": map[string]any{"ver": 1, "seq": 1, "val": title}}}}
		blob, _ := json.Marshal(cache)
		path := filepath.Join(source, "storages", "session_projcache", "sessions", id+".json")
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, blob, 0600)
	}
	workspace := map[string]any{"unit": map[string]any{"name": "workspace", "version": 2}, "global": map[string]any{"initialized": true, "defaultWorkspaceId": "demo-default", "workspaceIds": []string{"demo-project", "demo-default"}, "archivedSessionIds": []string{"session-demo-1"}, "pinnedSessionIds": []string{}}, "tables": map[string]any{"workspaces": map[string]any{"demo-project": map[string]any{"path": `C:\演示项目\DSH Space`, "title": "DSH Space", "sessionIds": []string{"session-demo-0", "session-demo-1"}, "createdAt": "2026-10-16T00:00:00Z", "updatedAt": "2026-10-16T00:00:00Z"}, "demo-default": map[string]any{"path": `C:\演示项目\default-workspace`, "title": "default-workspace", "sessionIds": []string{"session-demo-2"}, "createdAt": "2026-10-16T00:00:00Z", "updatedAt": "2026-10-16T00:00:00Z"}}}}
	workspaceBytes, _ := json.Marshal(workspace)
	if e = os.WriteFile(filepath.Join(source, "storages", "workspace.json"), workspaceBytes, 0600); e != nil {
		return nil, e
	}
	service, e := space.New(source, storage, func() error { return nil })
	return &App{service: service, demo: true}, e
}
func servePreview(app *App) error {
	var random [24]byte
	if _, e := rand.Read(random[:]); e != nil {
		return e
	}
	token := hex.EncodeToString(random[:])
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	host := listener.Addr().String()
	handler, e := previewHandler(app, host, token)
	if e != nil {
		listener.Close()
		return e
	}
	fmt.Printf("DSH_SPACE_PREVIEW http://%s/?preview=%s\n", host, token)
	return (&http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}).Serve(listener)
}
func previewHandler(app *App, host, token string) (http.Handler, error) {
	files, e := fs.Sub(assets, "frontend/dist")
	if e != nil {
		return nil, e
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(files)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Host != host || r.Header.Get("X-DSH-Preview") != token {
			http.Error(w, "forbidden", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "origin", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var args []json.RawMessage
		if json.NewDecoder(r.Body).Decode(&args) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		var result any
		var err error
		method := r.URL.Path[len("/api/"):]
		str := func(i int) string {
			var value string
			if i < len(args) {
				json.Unmarshal(args[i], &value)
			}
			return value
		}
		keys := func() []string {
			var value []string
			if len(args) > 0 {
				json.Unmarshal(args[0], &value)
			}
			return value
		}
		switch method {
		case "Settings":
			result = app.Settings()
		case "Conversations":
			result, err = app.Conversations()
		case "Read":
			offset := 0
			if len(args) > 1 {
				json.Unmarshal(args[1], &offset)
			}
			result, err = app.Read(str(0), offset)
		case "Image":
			result, err = app.Image(str(0), str(1))
		case "ImportBackup":
			result, err = app.ImportBackup()
		case "Backup":
			result, err = app.Backup(keys())
		case "PreviewHide":
			result, err = app.PreviewHide(keys())
		case "Hide":
			var request space.HideRequest
			if len(args) > 0 {
				json.Unmarshal(args[0], &request)
			}
			result, err = app.Hide(request)
		case "HideDirect":
			result, err = app.HideDirect(keys())
		case "PreviewDelete":
			var request space.DeleteRequest
			if len(args) > 0 {
				json.Unmarshal(args[0], &request)
			}
			result, err = app.PreviewDelete(request)
		case "DeleteConfirmed":
			result, err = app.DeleteConfirmed(str(0))
		case "Hidden":
			result, err = app.Hidden()
		case "Backups":
			result, err = app.Backups()
		case "RestoreHidden":
			err = app.RestoreHidden(str(0))
		case "RestoreBackup":
			err = app.RestoreBackup(str(0))
		case "DeleteBackup":
			err = app.DeleteBackup(str(0))
		case "ScanState":
			result = app.ScanState()
		case "StartScan":
			result = app.StartScan()
		case "CancelScan":
			app.CancelScan()
		case "ChooseSource":
			result, err = app.ChooseSource()
		case "OpenStorage":
			err = fmt.Errorf("演示目录仅显示，不打开资源管理器")
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})
	return mux, nil
}
