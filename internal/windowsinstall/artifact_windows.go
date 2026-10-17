// Package windowsinstall owns standalone Windows installation transactions.
package windowsinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const maxImage = 256 << 20

var versionToken = regexp.MustCompile(`^0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)
var identityToken = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type identity struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func NativeArchitecture() (string, error) {
	var processMachine, nativeMachine uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &processMachine, &nativeMachine); err != nil {
		return "", err
	}
	switch nativeMachine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "amd64", nil
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64", nil
	}
	return "", errors.New("unsupported native architecture")
}
func inspectImage(body []byte, version, arch string) (identity, error) {
	if len(body) == 0 || len(body) > maxImage || !versionToken.MatchString(version) {
		return identity{}, errors.New("invalid release image/version")
	}
	image, err := pe.NewFile(bytes.NewReader(body))
	if err != nil {
		return identity{}, err
	}
	defer image.Close()
	machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if arch == "arm64" {
		machine = pe.IMAGE_FILE_MACHINE_ARM64
	} else if arch != "amd64" {
		return identity{}, errors.New("unsupported image architecture")
	}
	if image.Machine != machine || image.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || image.Characteristics&pe.IMAGE_FILE_DLL != 0 {
		return identity{}, errors.New("wrong PE architecture or image type")
	}
	if _, ok := image.OptionalHeader.(*pe.OptionalHeader64); !ok {
		return identity{}, errors.New("PE32+ image required")
	}
	info, err := buildinfo.Read(bytes.NewReader(body))
	if err != nil {
		return identity{}, err
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if info.Path != "github.com/jinyongp/devtools/cmd/devtools" || settings["GOOS"] != "windows" || settings["GOARCH"] != arch || settings["CGO_ENABLED"] != "0" {
		return identity{}, errors.New("release build metadata mismatch")
	}
	prefix := []byte("devtools-build-v1:" + version + ":")
	start := bytes.Index(body, prefix)
	if start < 0 {
		return identity{}, errors.New("release version identity missing")
	}
	tail := body[start+len(prefix):]
	end := bytes.IndexByte(tail, ':')
	if end <= 0 || end > 128 {
		return identity{}, errors.New("invalid release commit")
	}
	commit := string(tail[:end])
	if !identityToken.MatchString(commit) {
		return identity{}, errors.New("invalid release commit")
	}
	expected := []byte("devtools-build-v1:" + version + ":" + commit + ":windows:" + arch + ":end")
	if bytes.Count(body, expected) != 1 || !bytes.HasPrefix(body[start:], expected) {
		return identity{}, errors.New("release linked identity mismatch")
	}
	return identity{version, commit}, nil
}
func smoke(ctx context.Context, path string, want identity) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "version")
	var output limitedOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return err
	}
	var response struct {
		OK   bool     `json:"ok"`
		Data identity `json:"data"`
	}
	if json.Unmarshal(output.body, &response) != nil || !response.OK || response.Data != want {
		return errors.New("release runtime version/commit mismatch")
	}
	return nil
}

type limitedOutput struct{ body []byte }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(w.body)+len(p) > 65536 {
		return 0, errors.New("release response too large")
	}
	w.body = append(w.body, p...)
	return len(p), nil
}
func checksum(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("release resource too large")
	}
	return body, nil
}
func fetch(ctx context.Context, source, name string, limit int64) ([]byte, error) {
	if strings.Contains(source, "://") {
		base, err := url.Parse(source)
		if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
			return nil, errors.New("release directory must use HTTPS")
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(source, "/")+"/"+name, nil)
		if err != nil {
			return nil, err
		}
		client := http.Client{Timeout: 120 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
				return errors.New("unsafe release redirect")
			}
			return nil
		}}
		response, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil, fmt.Errorf("release download status %d", response.StatusCode)
		}
		return readBounded(response.Body, limit)
	}
	file, err := os.Open(filepath.Join(source, name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("regular release source required")
	}
	return readBounded(file, limit)
}
func release(ctx context.Context, version, source, arch string) ([]byte, string, error) {
	if source != "" && version == "" {
		return nil, "", errors.New("source requires an explicit version")
	}
	if source == "" {
		base := os.Getenv("DEVTOOLS_RELEASE_URL")
		if base == "" {
			base = "https://github.com/jinyongp/devtools/releases"
		}
		parsed, err := url.Parse(base)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return nil, "", errors.New("release URL must use HTTPS")
		}
		if version == "" {
			data, err := fetch(ctx, strings.TrimRight(base, "/")+"/latest/download", "version.txt", 256)
			if err != nil {
				return nil, "", err
			}
			version = strings.TrimSpace(string(data))
		}
		source = strings.TrimRight(base, "/") + "/download/v" + version
	}
	if !versionToken.MatchString(version) {
		return nil, "", errors.New("invalid release version")
	}
	name := "devtools_" + version + "_windows_" + arch + ".zip"
	expected, err := fetch(ctx, source, name+".sha256", 1024)
	if err != nil {
		return nil, "", err
	}
	fields := strings.Fields(string(expected))
	if len(fields) != 2 || fields[1] != name || len(fields[0]) != 64 {
		return nil, "", errors.New("invalid artifact checksum")
	}
	archive, err := fetch(ctx, source, name, maxImage)
	if err != nil {
		return nil, "", err
	}
	if !strings.EqualFold(checksum(archive), fields[0]) {
		return nil, "", errors.New("artifact checksum mismatch")
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, "", err
	}
	if len(reader.File) != 1 {
		return nil, "", errors.New("ZIP must contain exactly one executable")
	}
	file := reader.File[0]
	if file.Name != "devtools.exe" || !file.Mode().IsRegular() || file.Flags&1 != 0 || file.UncompressedSize64 == 0 || file.UncompressedSize64 > maxImage {
		return nil, "", errors.New("invalid ZIP executable entry")
	}
	member, err := file.Open()
	if err != nil {
		return nil, "", err
	}
	defer member.Close()
	body, err := readBounded(member, maxImage)
	return body, version, err
}
