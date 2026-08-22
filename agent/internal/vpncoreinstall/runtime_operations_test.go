package vpncoreinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyNamedSHA256AcceptsMatchingAsset(t *testing.T) {
	payload := []byte("routegate-runtime")
	digest := sha256.Sum256(payload)
	checksums := []byte(hex.EncodeToString(digest[:]) + "  runtime.bin\n")
	if err := verifyNamedSHA256(payload, checksums, "runtime.bin"); err != nil {
		t.Fatalf("verifyNamedSHA256: %v", err)
	}
}

func TestVerifyNamedSHA256RejectsMismatch(t *testing.T) {
	payload := []byte("routegate-runtime")
	checksums := []byte("0000000000000000000000000000000000000000000000000000000000000000  runtime.bin\n")
	if err := verifyNamedSHA256(payload, checksums, "runtime.bin"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestExtractSingleRegularTarGzipFileRejectsLinksAndTraversal(t *testing.T) {
	for _, test := range []struct {
		name   string
		header tar.Header
	}{
		{name: "traversal", header: tar.Header{Name: "../mtg", Mode: 0o755, Size: 1, Typeflag: tar.TypeReg}},
		{name: "symlink", header: tar.Header{Name: "mtg-2.2.8-linux-amd64/mtg", Linkname: "/bin/sh", Typeflag: tar.TypeSymlink}},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := testTarGzip(t, test.header, []byte("x"))
			if _, err := extractSingleRegularTarGzipFile(archive, "mtg-2.2.8-linux-amd64/mtg"); err == nil {
				t.Fatal("expected unsafe archive to be rejected")
			}
		})
	}
}

func TestExtractSingleRegularTarGzipFileReturnsExpectedBinary(t *testing.T) {
	payload := []byte("mtg-binary")
	path := "mtg-2.2.8-linux-amd64/mtg"
	archive := testTarGzip(t, tar.Header{Name: path, Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}, payload)
	got, err := extractSingleRegularTarGzipFile(archive, path)
	if err != nil {
		t.Fatalf("extract runtime: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary = %q, want %q", got, payload)
	}
}

func TestRuntimeInstallationOperationNamesAreStable(t *testing.T) {
	got := []string{OperationInstall, OperationInstallWireGuard, OperationInstallHysteria2, OperationInstallMTG}
	want := []string{"install_sing_box", "install_wireguard", "install_hysteria2", "install_mtg"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("operation[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func testTarGzip(t *testing.T, header tar.Header, payload []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if header.Typeflag == tar.TypeReg && len(payload) > 0 {
		if _, err := tarWriter.Write(payload); err != nil {
			t.Fatalf("write tar payload: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return output.Bytes()
}
