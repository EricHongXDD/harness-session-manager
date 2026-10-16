package main

import (
	"bytes"
	"dsh-space/internal/space"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 验收真正的应用绑定与本地演示接口；来源固定为测试自有目录。
func TestDemoAPIBackupHideRestoreAndFrozenPreview(t *testing.T) {
	app, e := demoAppAt(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer app.service.Close()
	h, e := previewHandler(app, "127.0.0.1:12345", "test-token")
	if e != nil {
		t.Fatal(e)
	}
	call := func(method string, args ...any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(args)
		r := httptest.NewRequest("POST", "http://127.0.0.1:12345/api/"+method, bytes.NewReader(body))
		r.Header.Set("X-DSH-Preview", "test-token")
		r.Header.Set("Origin", "http://127.0.0.1:12345")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder, out any) {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("API失败: %d %s", w.Code, w.Body.String())
		}
		if json.Unmarshal(w.Body.Bytes(), out) != nil {
			t.Fatal(w.Body.String())
		}
	}
	var cfg space.Settings
	decode(call("Settings"), &cfg)
	if !cfg.Demo {
		t.Fatal("演示身份未显示")
	}
	if call("ChooseSource").Code != 400 {
		t.Fatal("演示来源可切换")
	}
	var c space.Catalog
	decode(call("Conversations"), &c)
	if len(c.Items) != 3 {
		t.Fatal(c)
	}
	var page space.MessagePage
	decode(call("Read", c.Items[0].Key, 0), &page)
	if len(page.Items) != 7 {
		t.Fatal(page)
	}
	for _, conversation := range c.Items {
		decode(call("Read", conversation.Key, 0), &page)
		if len(page.Items[0].Images) > 0 {
			var src string
			decode(call("Image", conversation.Key, page.Items[0].Images[0].ID), &src)
			if !bytes.HasPrefix([]byte(src), []byte("data:image/png;base64,")) {
				t.Fatal("图片 API 未返回本地栅格")
			}
		}
	}
	if call("ImportBackup").Code != 400 {
		t.Fatal("演示模式打开了真实文件选择器")
	}
	keys := []string{c.Items[0].Key}
	var manual space.Record
	decode(call("Backup", keys), &manual)
	var preview space.HidePreview
	decode(call("PreviewHide", keys), &preview)
	stale := space.HideRequest{Keys: keys, ExpectedKeys: []string{"stale"}, Source: preview.Source, Identity: preview.Identity}
	if call("Hide", stale).Code != 400 {
		t.Fatal("过期清单被执行")
	}
	var backups []space.Record
	decode(call("Backups"), &backups)
	if len(backups) != 1 {
		t.Fatal("过期清单不应新增备份")
	}
	request := space.HideRequest{Keys: keys, ExpectedKeys: []string{c.Items[0].Key}, Source: preview.Source, Identity: preview.Identity}
	var hidden space.Record
	decode(call("Hide", request), &hidden)
	decode(call("Conversations"), &c)
	if len(c.Items) != 2 {
		t.Fatal("隐藏会话仍在原目录列表")
	}
	var records []space.Record
	decode(call("Hidden"), &records)
	if len(records) != 1 || records[0].Bytes <= 0 || len(records[0].Moved) != 1 {
		t.Fatal(records)
	}
	decode(call("Backups"), &backups)
	if len(backups) != 1 {
		t.Fatal("隐藏不应创建 ZIP")
	}
	if call("RestoreHidden", hidden.ID).Code != 200 {
		t.Fatal("恢复失败")
	}
	decode(call("Conversations"), &c)
	if len(c.Items) != 3 {
		t.Fatal("恢复遗漏")
	}
	decode(call("Hidden"), &records)
	if len(records) != 0 {
		t.Fatal("恢复后仍保留隐藏记录", records)
	}
	if call("RestoreBackup", manual.ID).Code != 200 {
		t.Fatal("相同内容恢复应幂等")
	}
	if call("DeleteBackup", manual.ID).Code != 200 {
		t.Fatal("删除备份失败")
	}
	decode(call("Backups"), &backups)
	if len(backups) != 0 {
		t.Fatal(backups)
	}
}
func TestPreviewRejectsCrossOriginAndMissingToken(t *testing.T) {
	app, e := demoAppAt(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer app.service.Close()
	h, e := previewHandler(app, "127.0.0.1:12345", "token")
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ method, host, token, origin string }{{"GET", "127.0.0.1:12345", "token", ""}, {"POST", "127.0.0.1:12345", "", ""}, {"POST", "foreign.example", "token", ""}, {"POST", "127.0.0.1:12345", "token", "http://foreign.example"}} {
		r := httptest.NewRequest(tc.method, "http://"+tc.host+"/api/Conversations", bytes.NewBufferString("[]"))
		r.Header.Set("X-DSH-Preview", tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatal(tc, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:12345/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Harness Session Manager")) {
		t.Fatal("嵌入界面不可读取", w.Code)
	}
}
