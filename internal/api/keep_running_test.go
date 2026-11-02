package api

import (
	"net/http"
	"testing"
)

func TestKeepRunningSetting(t *testing.T) {
	database, handler, _ := chatAPI(t)
	if !KeepRunningOnClose(database) {
		t.Fatal("keep running should default to on")
	}
	var got keepRunningSettings
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/keep-running", keepRunningSettings{}, &got); code != http.StatusOK || got.KeepRunningOnClose {
		t.Fatalf("put = %d, %#v", code, got)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/keep-running", nil, &got); code != http.StatusOK || got.KeepRunningOnClose {
		t.Fatalf("get = %d, %#v", code, got)
	}
	if KeepRunningOnClose(database) {
		t.Fatal("saved off setting not read")
	}
}
