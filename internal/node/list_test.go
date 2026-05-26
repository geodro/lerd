package node

import (
	"reflect"
	"testing"
)

func TestParseFnmList_ExtractsMajorVersions(t *testing.T) {
	raw := "* v20.11.0 default\n  v18.20.0\n  v22.0.0\n"
	got := parseFnmList(raw)
	want := []string{"20", "18", "22"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFnmList() = %v, want %v", got, want)
	}
}

func TestParseFnmList_DedupesByMajor(t *testing.T) {
	raw := "  v20.11.0\n  v20.5.0\n  v18.0.0\n"
	got := parseFnmList(raw)
	want := []string{"20", "18"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFnmList() = %v, want %v", got, want)
	}
}

func TestParseFnmList_SkipsNonNumericMajors(t *testing.T) {
	raw := "  vlts/iron\n  v20.0.0\n"
	got := parseFnmList(raw)
	want := []string{"20"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFnmList() = %v, want %v", got, want)
	}
}

func TestParseFnmList_EmptyInput(t *testing.T) {
	if got := parseFnmList(""); got != nil {
		t.Errorf("parseFnmList(empty) = %v, want nil", got)
	}
}
