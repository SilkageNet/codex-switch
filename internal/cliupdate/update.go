package cliupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"golang.org/x/mod/semver"
)

const (
	latestReleaseURL = "https://api.github.com/repos/SilkageNet/codex-switch/releases/latest"
	maxMetadataBytes = 2 << 20
	maxDownloadBytes = 128 << 20
	maxBinaryBytes   = 64 << 20
)

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type Result struct {
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Current   bool   `json:"current"`
	Updated   bool   `json:"updated"`
}

func Run(ctx context.Context, installed string, checkOnly bool) (Result, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	latest, err := fetchRelease(ctx, client)
	if err != nil {
		return Result{}, err
	}
	result := Result{Installed: installed, Latest: latest.TagName}
	if isCurrent(installed, latest.TagName) {
		result.Current = true
		return result, nil
	}
	if checkOnly {
		return result, nil
	}

	archiveName := archiveName(latest.TagName, runtime.GOOS, runtime.GOARCH)
	archiveAsset, err := findAsset(latest, archiveName)
	if err != nil {
		return Result{}, err
	}
	checksumAsset, err := findAsset(latest, "checksums.txt")
	if err != nil {
		return Result{}, err
	}
	checksums, err := download(ctx, client, checksumAsset)
	if err != nil {
		return Result{}, fmt.Errorf("download checksums: %w", err)
	}
	expected, err := checksumFor(checksums, archiveName)
	if err != nil {
		return Result{}, err
	}
	archive, err := download(ctx, client, archiveAsset)
	if err != nil {
		return Result{}, fmt.Errorf("download release archive: %w", err)
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(actual[:], expected) {
		return Result{}, errors.New("release archive checksum did not match checksums.txt")
	}
	binary, err := extractBinary(archiveName, archive)
	if err != nil {
		return Result{}, err
	}
	if err := selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{}); err != nil {
		return Result{}, fmt.Errorf("replace current executable: %w", err)
	}
	result.Updated = true
	return result, nil
}

func fetchRelease(ctx context.Context, client *http.Client) (release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "codex-switch")
	response, err := client.Do(request)
	if err != nil {
		return release{}, fmt.Errorf("query latest release: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("query latest release: GitHub returned %s", response.Status)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return release{}, err
	}
	if len(encoded) > maxMetadataBytes {
		return release{}, errors.New("latest release metadata is too large")
	}
	var latest release
	if err := json.Unmarshal(encoded, &latest); err != nil {
		return release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if !semver.IsValid(latest.TagName) {
		return release{}, fmt.Errorf("latest release tag %q is not semantic versioning", latest.TagName)
	}
	return latest, nil
}

func isCurrent(installed, latest string) bool {
	current := installed
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}
	if !semver.IsValid(current) {
		return false
	}
	return semver.Compare(current, latest) >= 0
}

func archiveName(tag, goos, goarch string) string {
	version := strings.TrimPrefix(tag, "v")
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("codex-switch_%s_%s_%s%s", version, goos, goarch, extension)
}

func findAsset(latest release, name string) (asset, error) {
	for _, candidate := range latest.Assets {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return asset{}, fmt.Errorf("release asset %q was not found", name)
}

func download(ctx context.Context, client *http.Client, source asset) ([]byte, error) {
	if source.Size < 0 || source.Size > maxDownloadBytes {
		return nil, fmt.Errorf("release asset %q has an invalid size", source.Name)
	}
	parsed, err := url.Parse(source.BrowserDownloadURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return nil, fmt.Errorf("release asset %q has an untrusted URL", source.Name)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.BrowserDownloadURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "codex-switch")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %s for %s", response.Status, source.Name)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxDownloadBytes {
		return nil, fmt.Errorf("release asset %q is too large", source.Name)
	}
	return encoded, nil
}

func checksumFor(checksums []byte, name string) ([]byte, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			decoded, err := hex.DecodeString(fields[0])
			if err != nil || len(decoded) != sha256.Size {
				return nil, fmt.Errorf("checksum for %q is invalid", name)
			}
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("checksum for %q was not found", name)
}

func extractBinary(name string, archive []byte) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractZip(archive)
	}
	if strings.HasSuffix(name, ".tar.gz") {
		return extractTarGzip(archive)
	}
	return nil, errors.New("unsupported release archive")
}

func extractZip(encoded []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		return nil, fmt.Errorf("open release zip: %w", err)
	}
	for _, entry := range reader.File {
		if filepath.Base(entry.Name) != "codex-switch.exe" || entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > maxBinaryBytes {
			return nil, errors.New("release executable is too large")
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, err
		}
		binary, readErr := readBinary(stream)
		closeErr := stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return binary, nil
	}
	return nil, errors.New("codex-switch.exe was not found in the release archive")
}

func extractTarGzip(encoded []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("open release gzip: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release tar: %w", err)
		}
		if filepath.Base(header.Name) != "codex-switch" || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size < 0 || header.Size > maxBinaryBytes {
			return nil, errors.New("release executable is too large")
		}
		return readBinary(tarReader)
	}
	return nil, errors.New("codex-switch was not found in the release archive")
}

func readBinary(reader io.Reader) ([]byte, error) {
	binary, err := io.ReadAll(io.LimitReader(reader, maxBinaryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(binary) == 0 || len(binary) > maxBinaryBytes {
		return nil, errors.New("release executable has an invalid size")
	}
	return binary, nil
}
