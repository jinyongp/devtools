package profilekey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jinyongp/devtools/internal/project"
)

const (
	MarkerName = "profile-key-v1"
	Version    = 1
)

type LegacyStatus uint8

const (
	LegacyExisting LegacyStatus = iota + 1
	LegacyMissing
	LegacyUnaddressable
)

type Identity struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
	Profile string `json:"profile"`
	Domain  string `json:"domain"`
}

type Marker struct {
	StorageMarker string `json:"storage_marker"`
	Profile       string `json:"profile"`
	CanonicalKey  string `json:"canonical_key"`
}

func Key(profile string) string {
	sum := sha256.Sum256([]byte(profile))
	return "p1-" + hex.EncodeToString(sum[:])
}

func ValidKey(key string) bool {
	if len(key) != 67 || key[:3] != "p1-" {
		return false
	}
	_, err := hex.DecodeString(key[3:])
	return err == nil && key == strings.ToLower(key)
}

func CanonicalPath(directory, profile string) string {
	return filepath.Join(directory, Key(profile)+".json")
}

func IdentityPath(directory, profile string) string {
	return filepath.Join(directory, ".identity", Key(profile)+".json")
}

func LegacyPath(directory, profile string) string {
	return filepath.Join(directory, hex.EncodeToString([]byte(profile))+".json")
}

func LegacyFilename(profile string) string {
	return hex.EncodeToString([]byte(profile)) + ".json"
}

func CanonicalFilename(profile string) string {
	return Key(profile) + ".json"
}

func IdentityBytes(profile, domain string) ([]byte, error) {
	if !project.ValidProfile(profile) || domain != "values" && domain != "tasks" {
		return nil, errors.New("invalid profile identity")
	}
	return json.Marshal(Identity{Version: Version, Key: Key(profile), Profile: profile, Domain: domain})
}

func MarkerBytes(profile string) ([]byte, error) {
	if !project.ValidProfile(profile) {
		return nil, errors.New("invalid profile")
	}
	return json.Marshal(Marker{StorageMarker: MarkerName, Profile: profile, CanonicalKey: Key(profile)})
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private directory required")
	}
	return nil
}

func privateFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		_ = file.Close()
		return nil, errors.New("private file required")
	}
	return file, nil
}

func LegacyPathStatus(directory, profile string) (LegacyStatus, error) {
	if !project.ValidProfile(profile) {
		return 0, errors.New("invalid profile")
	}
	if err := privateDirectory(directory); err != nil {
		return 0, err
	}
	path := LegacyPath(directory, profile)
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return LegacyExisting, nil
	case errors.Is(err, os.ErrNotExist):
		return LegacyMissing, nil
	case errors.Is(err, syscall.ENAMETOOLONG):
		return LegacyUnaddressable, nil
	default:
		return 0, err
	}
}

func ReadIdentity(directory, profile, domain string) (bool, error) {
	if err := privateDirectory(filepath.Join(directory, ".identity")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	path := IdentityPath(directory, profile)
	file, err := privateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var identity Identity
	if err := decoder.Decode(&identity); err != nil || decoder.Decode(new(any)) != io.EOF {
		return false, errors.New("invalid profile identity")
	}
	expected := Key(profile)
	if identity.Version != Version || identity.Key != expected || identity.Profile != profile || identity.Domain != domain {
		return false, errors.New("profile identity mismatch")
	}
	return true, nil
}

func ReadMarker(path, profile string) (bool, error) {
	file, err := privateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var marker Marker
	if err := decoder.Decode(&marker); err != nil || decoder.Decode(new(any)) != io.EOF {
		return false, nil
	}
	if marker.StorageMarker == "" {
		return false, nil
	}
	if marker.StorageMarker != MarkerName || marker.Profile != profile || marker.CanonicalKey != Key(profile) {
		return true, errors.New("profile marker mismatch")
	}
	return true, nil
}

func EnumerateIdentities(directory, domain string) ([]string, error) {
	identityDir := filepath.Join(directory, ".identity")
	info, err := os.Lstat(identityDir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("invalid identity directory")
	}
	entries, err := os.ReadDir(identityDir)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			return nil, errors.New("invalid identity entry")
		}
		// Interrupted atomic writes can leave private .write-* temporary files.
		// Only published JSON files participate in identity enumeration.
		if filepath.Ext(name) != ".json" {
			continue
		}
		key := name[:len(name)-5]
		if !ValidKey(key) {
			return nil, errors.New("invalid identity key")
		}
		file, err := privateFile(filepath.Join(identityDir, name))
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		var identity Identity
		decodeErr := decoder.Decode(&identity)
		trailingErr := decoder.Decode(new(any))
		_ = file.Close()
		if decodeErr != nil || trailingErr != io.EOF || identity.Version != Version || identity.Key != key || identity.Domain != domain || !project.ValidProfile(identity.Profile) || Key(identity.Profile) != key {
			return nil, errors.New("invalid identity entry")
		}
		result = append(result, identity.Profile)
	}
	return result, nil
}

func DecodeLegacyFilename(name string) (string, bool) {
	if filepath.Ext(name) != ".json" {
		return "", false
	}
	raw, err := hex.DecodeString(name[:len(name)-5])
	if err != nil || !project.ValidProfile(string(raw)) {
		return "", false
	}
	return string(raw), true
}

type Mode uint8

const (
	ModeNone Mode = iota
	ModeLegacy
	ModeCanonical
)

type Resolution struct {
	Mode          Mode
	CanonicalPath string
	IdentityPath  string
	LegacyPath    string
	LegacyStatus  LegacyStatus
	Marker        bool
}

func canonicalFileExists(path string) (bool, error) {
	file, err := privateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, file.Close()
}

func Resolve(directory, profile, domain string) (Resolution, error) {
	result := Resolution{
		CanonicalPath: CanonicalPath(directory, profile),
		IdentityPath:  IdentityPath(directory, profile),
		LegacyPath:    LegacyPath(directory, profile),
	}
	if !project.ValidProfile(profile) || domain != "values" && domain != "tasks" {
		return result, errors.New("invalid profile resolution")
	}
	if err := privateDirectory(directory); err != nil {
		return result, err
	}
	canonical, err := canonicalFileExists(result.CanonicalPath)
	if err != nil {
		return result, err
	}
	identity, err := ReadIdentity(directory, profile, domain)
	if err != nil {
		return result, err
	}
	if canonical != identity {
		return result, errors.New("incomplete canonical profile pair")
	}
	status, err := LegacyPathStatus(directory, profile)
	if err != nil {
		return result, err
	}
	result.LegacyStatus = status
	if status == LegacyExisting {
		marker, markerErr := ReadMarker(result.LegacyPath, profile)
		if markerErr != nil {
			return result, markerErr
		}
		result.Marker = marker
	}
	if canonical {
		switch status {
		case LegacyUnaddressable:
			result.Mode = ModeCanonical
			return result, nil
		case LegacyExisting:
			if !result.Marker {
				return result, errors.New("canonical and legacy profile states conflict")
			}
			result.Mode = ModeCanonical
			return result, nil
		case LegacyMissing:
			return result, errors.New("canonical profile is missing downgrade marker")
		default:
			return result, errors.New("invalid legacy status")
		}
	}
	switch status {
	case LegacyExisting:
		if result.Marker {
			return result, errors.New("orphan profile migration marker")
		}
		result.Mode = ModeLegacy
	case LegacyMissing, LegacyUnaddressable:
		result.Mode = ModeNone
	default:
		return result, errors.New("invalid legacy status")
	}
	return result, nil
}

func StateRelative(storageDomain, profile string) string {
	return storageDomain + "/" + CanonicalFilename(profile)
}

func IdentityRelative(storageDomain, profile string) string {
	return storageDomain + "/.identity/" + CanonicalFilename(profile)
}

func LegacyRelative(storageDomain, profile string) string {
	return storageDomain + "/" + LegacyFilename(profile)
}

func Enumerate(directory, domain string) ([]string, error) {
	if domain != "values" && domain != "tasks" {
		return nil, errors.New("invalid profile domain")
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private directory required")
	}
	identityProfiles, err := EnumerateIdentities(directory, domain)
	if err != nil {
		return nil, err
	}
	seenProfiles := map[string]bool{}
	seenKeys := map[string]bool{}
	for _, profile := range identityProfiles {
		resolution, err := Resolve(directory, profile, domain)
		if err != nil || resolution.Mode != ModeCanonical {
			return nil, errors.New("invalid canonical profile storage")
		}
		seenProfiles[profile] = true
		seenKeys[Key(profile)] = true
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if entry.Name() == ".identity" {
				continue
			}
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		if strings.HasPrefix(base, "p1-") {
			if !ValidKey(base) || !seenKeys[base] {
				return nil, errors.New("canonical state missing identity")
			}
			continue
		}
		profile, ok := DecodeLegacyFilename(name)
		if !ok {
			return nil, errors.New("invalid legacy profile filename")
		}
		resolution, err := Resolve(directory, profile, domain)
		if err != nil {
			return nil, err
		}
		switch resolution.Mode {
		case ModeLegacy:
			seenProfiles[profile] = true
		case ModeCanonical:
			if !resolution.Marker {
				return nil, errors.New("invalid migrated profile marker")
			}
		case ModeNone:
			return nil, errors.New("profile filename has no logical state")
		}
	}
	result := make([]string, 0, len(seenProfiles))
	for profile := range seenProfiles {
		result = append(result, profile)
	}
	sort.Strings(result)
	return result, nil
}
