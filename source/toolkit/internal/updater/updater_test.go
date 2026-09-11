package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestLookupChecksum(t *testing.T) {
	body := []byte("aaa  agtk_1.0.0_linux_amd64.tar.gz\nbbb  agtk_1.0.0_darwin_arm64.tar.gz\n")
	got, err := lookupChecksum(body, "agtk_1.0.0_darwin_arm64.tar.gz")
	if err != nil {
		t.Fatalf("lookupChecksum: %v", err)
	}
	if got != "bbb" {
		t.Errorf("got %q, want bbb", got)
	}
}

func TestLookupChecksum_Missing(t *testing.T) {
	body := []byte("aaa  agtk_1.0.0_linux_amd64.tar.gz\n")
	_, err := lookupChecksum(body, "agtk_1.0.0_darwin_arm64.tar.gz")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "darwin_arm64") {
		t.Errorf("error should name the missing entry: %v", err)
	}
}

func TestExtractTarGz_FindsBinary(t *testing.T) {
	archive := makeTarGz(t, map[string]string{
		"agtk":      "binary-bytes",
		"README.md": "readme contents",
	})
	got, err := extractTarGz(archive, "agtk")
	if err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	if string(got) != "binary-bytes" {
		t.Errorf("got %q, want %q", string(got), "binary-bytes")
	}
}

func TestExtractTarGz_NotFound(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"README.md": "x"})
	if _, err := extractTarGz(archive, "agtk"); err == nil {
		t.Fatal("expected error when binary is absent")
	}
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Size: int64(len(content)), Mode: 0o755}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
