package cliupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestArchiveName(t *testing.T) {
	if got := archiveName("v1.2.3", "darwin", "arm64"); got != "codex-switch_1.2.3_darwin_arm64.tar.gz" {
		t.Fatalf("unexpected archive name: %s", got)
	}
	if got := archiveName("v1.2.3", "windows", "amd64"); got != "codex-switch_1.2.3_windows_amd64.zip" {
		t.Fatalf("unexpected archive name: %s", got)
	}
}

func TestChecksumFor(t *testing.T) {
	payload := []byte("archive")
	sum := sha256.Sum256(payload)
	encoded := []byte(fmt.Sprintf("%x  codex-switch.tar.gz\n", sum))
	got, err := checksumFor(encoded, "codex-switch.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sum[:]) {
		t.Fatal("unexpected checksum")
	}
}

func TestExtractTarGzip(t *testing.T) {
	var encoded bytes.Buffer
	gzipWriter := gzip.NewWriter(&encoded)
	tarWriter := tar.NewWriter(gzipWriter)
	want := []byte("binary")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "codex-switch", Mode: 0o755, Size: int64(len(want)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractTarGzip(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("unexpected binary")
	}
}

func TestExtractZip(t *testing.T) {
	var encoded bytes.Buffer
	writer := zip.NewWriter(&encoded)
	entry, err := writer.Create("codex-switch.exe")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("binary")
	if _, err := entry.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractZip(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("unexpected binary")
	}
}

func TestIsCurrent(t *testing.T) {
	if !isCurrent("1.2.3", "v1.2.3") || !isCurrent("v1.3.0", "v1.2.3") || isCurrent("dev", "v1.2.3") {
		t.Fatal("unexpected version comparison")
	}
}
