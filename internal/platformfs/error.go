package platformfs

import (
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/storageerr"
)

func storageResource(path string) string {
	switch strings.ToLower(filepath.Base(path)) {
	case "data":
		return "data_root"
	case "config":
		return "config_root"
	case "cache":
		return "cache_root"
	case "runtime":
		return "runtime_root"
	case ".maintenance":
		return "maintenance_root"
	case "tasks":
		return "task_store"
	case "profiles":
		return "profile_store"
	case "vault":
		return "vault_store"
	case "task-queries":
		return "query_cache"
	}
	return "private_storage"
}

func fsError(operation, path, reason string, cause error) error {
	return storageerr.Wrap(operation, storageResource(path), reason, cause)
}

func securityError(directory bool, reason string, cause error) error {
	resource := "private_file"
	if directory {
		resource = "private_directory"
	}
	return storageerr.Wrap("check_security", resource, reason, cause)
}
