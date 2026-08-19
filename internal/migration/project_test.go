package migration

import (
	"reflect"
	"testing"
)

func TestBuildMapping(t *testing.T) {
	rows := []ProjectRow{
		{ID: 1, Name: "小火龙扫码登录器"},
		{ID: 2, Name: "另一项目"},
		{ID: 3, Name: "项目C"},
	}
	got, err := BuildMapping(rows, "小火龙", "100001")
	if err != nil {
		t.Fatalf("BuildMapping error: %v", err)
	}
	want := map[uint64]string{1: "100001", 2: "000002", 3: "000003"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapping = %v, want %v", got, want)
	}
}

func TestBuildMappingFallbackID1(t *testing.T) {
	rows := []ProjectRow{
		{ID: 1, Name: "扫码项目"},
		{ID: 7, Name: "项目B"},
	}
	got, err := BuildMapping(rows, "不存在的关键字", "100001")
	if err != nil {
		t.Fatalf("BuildMapping error: %v", err)
	}
	if got[1] != "100001" || got[7] != "000007" {
		t.Fatalf("fallback id=1 应映射为 100001: %v", got)
	}
}

func TestBuildMappingNoTarget(t *testing.T) {
	rows := []ProjectRow{{ID: 5, Name: "项目A"}}
	if _, err := BuildMapping(rows, "小火龙", "100001"); err == nil {
		t.Fatal("无目标项目应报错")
	}
}

func TestBuildMappingCollision(t *testing.T) {
	rows := []ProjectRow{
		{ID: 1, Name: "小火龙"},
		{ID: 100001, Name: "冲突项目"},
	}
	if _, err := BuildMapping(rows, "小火龙", "100001"); err == nil {
		t.Fatal("新 id 冲突应报错")
	}
}
