package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// apk_format_test.go (relocated from charly/apk_format_test.go's
// TestCompileApkStep, #55 K3 Cone 4): verifies the candy `apk:` package format
// compiles into a single ApkInstallStep carrying every entry, and that an
// empty apk: list compiles to nothing. Pure deploykit + literal spec
// fixtures, no charly loader machinery needed.

func TestCompileApkStep(t *testing.T) {
	none := newTestCandy("no-apk", spec.CandyModel{})
	if step := CompileApkStep(none); step != nil {
		t.Errorf("candy with no apk: should compile to nil step, got %T", step)
	}

	l := newTestCandy("test-apps", spec.CandyModel{
		SourceDir: "/layers/test-apps",
		Apk: []ApkPackageSpec{
			{Package: "org.fdroid.fdroid", Source: "apk-pure", Arch: "x86_64"},
			{Apk: "tests/data/x.apk"},
		},
	})
	step := CompileApkStep(l)
	if step == nil {
		t.Fatal("CompileApkStep returned nil for a candy with apk: entries")
	}
	apk, ok := step.(*ApkInstallStep)
	if !ok {
		t.Fatalf("CompileApkStep returned %T, want *ApkInstallStep", step)
	}
	if apk.Kind() != spec.StepKindApkInstall {
		t.Errorf("Kind() = %q, want %q", apk.Kind(), spec.StepKindApkInstall)
	}
	if len(apk.Packages) != 2 {
		t.Errorf("Packages len = %d, want 2", len(apk.Packages))
	}
	if apk.CandyName != "test-apps" || apk.CandyDir != "/layers/test-apps" {
		t.Errorf("CandyName/CandyDir = %q/%q", apk.CandyName, apk.CandyDir)
	}
	if apk.Reverse() != nil {
		t.Errorf("ApkInstallStep.Reverse() should be nil (android teardown ops are dynamic, recorded from the deploy:android plugin reply)")
	}
}
