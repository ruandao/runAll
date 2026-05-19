package runall_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "src", "main.go")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("runAll module root not found")
		}
		dir = parent
	}
}

func TestBuildScriptProducesBinary(t *testing.T) {
	root := moduleRoot(t)
	bin := filepath.Join(root, "bin", "runAll")
	_ = os.Remove(bin)

	cmd := exec.Command("bash", "build.sh")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build.sh failed: %v\n%s", err, out)
	}

	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("binary missing: %v\nbuild output:\n%s", err, out)
	}
	if info.IsDir() {
		t.Fatal("bin/runAll is a directory")
	}
	if info.Size() == 0 {
		t.Fatal("bin/runAll is empty")
	}
}

func TestBuiltBinaryAcceptsConfigFlag(t *testing.T) {
	root := moduleRoot(t)
	bin := filepath.Join(root, "bin", "runAll")

	// Missing config file should fail fast with non-zero exit.
	cmd := exec.Command(bin, "--config", "/nonexistent/config.yaml", "--daemon")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit with bad config, out=%s", out)
	}
}
