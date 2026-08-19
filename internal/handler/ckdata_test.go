package handler

import (
	"reflect"
	"testing"

	"xhl-server/internal/model"
)

func TestBuildExportLines(t *testing.T) {
	rows := []model.CkData{
		{ID: 1, Username: "u1", Password: "p1", Cookie: "BDUSS=a; STOKEN=x"},
		{ID: 2, Username: "u2", Password: "p2", Cookie: "BDUSS=b; STOKEN=y"},
	}
	got := buildExportLines(rows)
	want := []string{"u1----p1----BDUSS=a; STOKEN=x", "u2----p2----BDUSS=b; STOKEN=y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildExportLines = %v, want %v", got, want)
	}
}

func TestBuildExportLinesEmpty(t *testing.T) {
	if got := buildExportLines(nil); len(got) != 0 {
		t.Fatalf("空行应返回空列表: %v", got)
	}
}
