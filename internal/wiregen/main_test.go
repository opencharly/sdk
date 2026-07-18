package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	protobuf "github.com/emicklei/proto"
)

func TestGeneratedProtocolReproducible(t *testing.T) {
	model, err := loadModel(filepath.Join("..", "..", "protocol", "schema"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateModel(&model); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "proto", "plugin.proto"))
	if err != nil {
		t.Fatal(err)
	}
	if got := renderProto(&model); !bytes.Equal(got, want) {
		t.Fatal("proto/plugin.proto drifted from protocol/schema/*.cue; run task wire:gen")
	}
}

func TestCurrentProtocolRoundTrip(t *testing.T) {
	path := filepath.Join("..", "..", "proto", "plugin.proto")
	original := parseProtoModel(t, path)
	if err := validateModel(&original); err != nil {
		t.Fatalf("validate original: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "plugin.proto")
	if err := os.WriteFile(tmp, renderProto(&original), 0o644); err != nil {
		t.Fatal(err)
	}
	roundTrip := parseProtoModel(t, tmp)
	if !reflect.DeepEqual(original, roundTrip) {
		t.Fatalf("protobuf model changed on CUE-generator round trip\noriginal: %#v\nroundtrip: %#v", original, roundTrip)
	}
}

func TestValidateModelRejectsDuplicateFieldNumber(t *testing.T) {
	model := protocolModel{
		Syntax: "proto3", Package: "test", GoPackage: "example.test/proto",
		Messages: []messageModel{{Name: "Request", Fields: []fieldModel{
			{Name: "first", Type: "string", Number: 1},
			{Name: "second", Type: "string", Number: 1},
		}}},
	}
	if err := validateModel(&model); err == nil {
		t.Fatal("expected duplicate field number to fail")
	}
}

func parseProtoModel(t *testing.T, path string) protocolModel {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close protocol fixture: %v", err)
		}
	})
	parsed, err := protobuf.NewParser(f).Parse()
	if err != nil {
		t.Fatal(err)
	}
	model, err := modelFromProto(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return model
}
