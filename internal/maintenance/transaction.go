package maintenance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const transactionVersion = 2

type transactionPointer struct {
	Version       int    `json:"version"`
	TransactionID string `json:"transaction_id"`
	Phase         string `json:"phase"`
}

type transactionEntry struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	BeforePath string `json:"before_path,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

type transactionManifest struct {
	Version       int                `json:"version"`
	TransactionID string             `json:"transaction_id"`
	Entries       []transactionEntry `json:"entries"`
}

func transactionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func validTransactionID(id string) bool {
	compact := strings.ReplaceAll(id, "-", "")
	if len(id) != 36 || len(compact) != 32 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}

func validKey(name string) bool {
	if !strings.HasPrefix(name, "p1-") || len(name) != 67 {
		return false
	}
	_, err := hex.DecodeString(name[3:])
	return err == nil
}

func validReplacementPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) == 2 {
		if parts[0] != "profiles" && parts[0] != "tasks" && parts[0] != "backup-receipts" {
			return false
		}
		return parts[1] != ".json" && strings.HasSuffix(parts[1], ".json") && !strings.Contains(parts[1], "..")
	}
	if len(parts) == 3 {
		if parts[0] != "profiles" && parts[0] != "tasks" || parts[1] != ".identity" {
			return false
		}
		if !strings.HasSuffix(parts[2], ".json") {
			return false
		}
		return validKey(strings.TrimSuffix(parts[2], ".json"))
	}
	return false
}

func privateDirectoryComponent(path string, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private directory required")
	}
	return nil
}

func targetPath(root, relative string, createParents bool) (string, error) {
	if !validReplacementPath(relative) {
		return "", errors.New("invalid replacement path")
	}
	if err := privateDirectoryComponent(root, false); err != nil {
		return "", err
	}
	parts := strings.Split(relative, "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		if err := privateDirectoryComponent(current, createParents); err != nil {
			return "", err
		}
	}
	return filepath.Join(current, parts[len(parts)-1]), nil
}

func privateFileInfo(file *os.File) (os.FileInfo, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private file required")
	}
	return info, nil
}

func createTransactionDirectories(root, id string) (string, string, error) {
	maintenanceDir := filepath.Join(root, ".maintenance")
	if err := privateDirectoryComponent(maintenanceDir, false); err != nil {
		return "", "", err
	}
	transactions := filepath.Join(maintenanceDir, "transactions")
	if err := privateDirectoryComponent(transactions, true); err != nil {
		return "", "", err
	}
	dir := filepath.Join(transactions, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", "", err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
			_ = syncDir(transactions)
		}
	}()
	if err := privateDirectoryComponent(dir, false); err != nil {
		return "", "", err
	}
	before := filepath.Join(dir, "before")
	if err := os.Mkdir(before, 0700); err != nil {
		return "", "", err
	}
	if err := privateDirectoryComponent(before, false); err != nil {
		return "", "", err
	}
	if err := syncDir(transactions); err != nil {
		return "", "", err
	}
	if err := syncDir(dir); err != nil {
		return "", "", err
	}
	success = true
	return dir, before, nil
}

func copyPrivateFile(source, destination string) (int64, string, error) {
	src, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, "", err
	}
	defer src.Close()
	if _, err := privateFileInfo(src); err != nil {
		return 0, "", err
	}
	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return 0, "", err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dst, hash), src)
	if copyErr == nil {
		copyErr = dst.Sync()
	}
	closeErr := dst.Close()
	if copyErr != nil {
		return 0, "", copyErr
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyBeforeImage(transactionDir string, entry transactionEntry) error {
	if !entry.Exists {
		if entry.BeforePath != "" || entry.Size != 0 || entry.Digest != "" {
			return errors.New("invalid absent before image")
		}
		return nil
	}
	if entry.BeforePath == "" || entry.Size < 0 || len(entry.Digest) != sha256.Size*2 {
		return errors.New("invalid before image metadata")
	}
	if filepath.Base(entry.BeforePath) != entry.BeforePath || !strings.HasPrefix(entry.BeforePath, "before-") {
		return errors.New("invalid before image path")
	}
	path := filepath.Join(transactionDir, "before", entry.BeforePath)
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := privateFileInfo(file)
	if err != nil || info.Size() != entry.Size {
		return errors.New("invalid before image")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil || size != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.Digest {
		return errors.New("before image digest mismatch")
	}
	return nil
}

func restorePrivateFile(source, target string) error {
	parent := filepath.Dir(target)
	temp, err := os.CreateTemp(parent, ".restore-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	src, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := privateFileInfo(src); err != nil {
		_ = src.Close()
		_ = temp.Close()
		return err
	}
	_, copyErr := io.Copy(temp, src)
	closeSrcErr := src.Close()
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeTempErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSrcErr != nil {
		return closeSrcErr
	}
	if closeTempErr != nil {
		return closeTempErr
	}
	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	return syncDir(parent)
}

func removeTarget(target string) error {
	err := os.Remove(target)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
}

func transactionsRoot(root string) string {
	return filepath.Join(root, ".maintenance", "transactions")
}

func transactionDir(root, id string) string {
	return filepath.Join(transactionsRoot(root), id)
}

func pendingPath(root string) string {
	return filepath.Join(root, ".maintenance", "restore-pending.json")
}

func readPointer(root string) (transactionPointer, bool, []byte, error) {
	var pointer transactionPointer
	body, err := Read(pendingPath(root), 512<<20)
	if errors.Is(err, os.ErrNotExist) {
		return pointer, false, nil, nil
	}
	if err != nil {
		return pointer, false, nil, err
	}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		return pointer, true, body, nil
	}
	if json.Unmarshal(body, &pointer) != nil || pointer.Version != transactionVersion || !validTransactionID(pointer.TransactionID) || pointer.Phase != "applying" && pointer.Phase != "committed" {
		return pointer, false, nil, errors.New("invalid recovery journal")
	}
	return pointer, false, body, nil
}

func readManifest(root string, pointer transactionPointer) (transactionManifest, error) {
	var manifest transactionManifest
	dir := transactionDir(root, pointer.TransactionID)
	if err := privateDirectoryComponent(dir, false); err != nil {
		return manifest, err
	}
	body, err := Read(filepath.Join(dir, "manifest.json"), 16<<20)
	if err != nil {
		return manifest, err
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.Version != transactionVersion || manifest.TransactionID != pointer.TransactionID || len(manifest.Entries) == 0 {
		return manifest, errors.New("invalid transaction manifest")
	}
	seen := map[string]bool{}
	beforePaths := map[string]bool{}
	for _, entry := range manifest.Entries {
		if !validReplacementPath(entry.Path) || seen[entry.Path] {
			return manifest, errors.New("invalid transaction target")
		}
		seen[entry.Path] = true
		if entry.BeforePath != "" {
			if beforePaths[entry.BeforePath] {
				return manifest, errors.New("duplicate before image")
			}
			beforePaths[entry.BeforePath] = true
		}
		if _, err := targetPath(root, entry.Path, false); err != nil && !errors.Is(err, os.ErrNotExist) {
			return manifest, err
		}
		if err := verifyBeforeImage(dir, entry); err != nil {
			return manifest, err
		}
	}
	return manifest, nil
}

func cleanupTransaction(root string, pointer transactionPointer) error {
	pending := pendingPath(root)
	observed, legacy, raw, err := readPointer(root)
	if err != nil {
		return err
	}
	if raw != nil {
		if legacy || observed.TransactionID != pointer.TransactionID || observed.Phase != pointer.Phase {
			return errors.New("unexpected recovery pointer")
		}
		if err := os.Remove(pending); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncDir(filepath.Dir(pending)); err != nil {
			return err
		}
	}
	dir := transactionDir(root, pointer.TransactionID)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(transactionsRoot(root)); err == nil && info.IsDir() {
		return syncDir(transactionsRoot(root))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func recoverTransaction(root string, pointer transactionPointer) error {
	if pointer.Phase == "committed" {
		if err := privateDirectoryComponent(transactionDir(root, pointer.TransactionID), false); err != nil {
			return err
		}
		return cleanupTransaction(root, pointer)
	}
	manifest, err := readManifest(root, pointer)
	if err != nil {
		return err
	}
	for _, entry := range manifest.Entries {
		target, err := targetPath(root, entry.Path, true)
		if err != nil {
			return err
		}
		if entry.Exists {
			source := filepath.Join(transactionDir(root, pointer.TransactionID), "before", entry.BeforePath)
			if err := restorePrivateFile(source, target); err != nil {
				return err
			}
		} else if err := removeTarget(target); err != nil {
			return err
		}
	}
	return cleanupTransaction(root, pointer)
}

func cleanupOrphanTransactions(root, active string) error {
	parent := transactionsRoot(root)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("invalid transaction directory")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		if !validTransactionID(entry.Name()) {
			return errors.New("invalid transaction directory")
		}
		path := filepath.Join(parent, entry.Name())
		if err := privateDirectoryComponent(path, false); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return syncDir(parent)
}
