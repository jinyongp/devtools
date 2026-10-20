package platformfs

import (
	"errors"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// storageIdentity is the single policy for creating and checking private
// storage objects. Ownership remains with the user; the exact trustee set also
// includes SYSTEM and, inside an AppContainer, that process's container SID.
type storageIdentity struct {
	owner    *windows.SID
	trustees []*windows.SID
}

// A container's Known Folder is an OS-provided trust boundary: Windows creates
// its per-user/per-container profile and prevents other containers from accessing
// it. The owning host user and SYSTEM already control the storage policy. Do not
// infer this boundary from a path spelling or fall back to it after access denial.
// Outside it, the usual drive-root traversal and ancestor pins still apply.
func storageDirectoryStart(path string) (string, error) {
	drive := filepath.VolumeName(path) + `\`
	app, err := storageAppContainerSID()
	if err != nil {
		return "", err
	}
	if app == nil {
		return drive, nil
	}
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return "", err
	}
	base, err = privatePath(base)
	if err != nil {
		return "", err
	}
	if storagePathWithin(base, path) {
		return base, nil
	}
	return drive, nil
}

func storagePathWithin(base, path string) bool {
	if base == "" {
		return false
	}
	relative, err := filepath.Rel(base, path)
	return err == nil && (relative == "." || filepath.IsLocal(relative))
}

func currentStorageIdentity() (*storageIdentity, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	app, err := storageAppContainerSID()
	if err != nil {
		return nil, err
	}
	identity := &storageIdentity{owner: user.User.Sid, trustees: []*windows.SID{user.User.Sid, system}}
	if app != nil {
		identity.trustees = append(identity.trustees, app)
	}
	return identity, nil
}

func (identity *storageIdentity) descriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	sddl := "O:" + identity.owner.String() + "D:P"
	for _, trustee := range identity.trustees {
		sddl += "(A;OICI;FA;;;" + trustee.String() + ")"
	}
	return windows.SecurityDescriptorFromString(sddl)
}

func privateSecurity() (*windows.SID, *windows.SECURITY_DESCRIPTOR, error) {
	identity, err := currentStorageIdentity()
	if err != nil {
		return nil, nil, err
	}
	sd, err := identity.descriptor()
	return identity.owner, sd, err
}

func (identity *storageIdentity) check(sd *windows.SECURITY_DESCRIPTOR, directory bool) error {
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !windows.EqualSid(owner, identity.owner) {
		return securityError(directory, "owner_mismatch", ErrUnsafe)
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if directory && control&windows.SE_DACL_PROTECTED == 0 {
		return securityError(directory, "inheritance_enabled", ErrUnsafe)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil || int(acl.AceCount) != len(identity.trustees) {
		return securityError(directory, "unexpected_acl", ErrUnsafe)
	}
	seen := make([]bool, len(identity.trustees))
	for index := uint32(0); index < uint32(acl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, index, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Mask&0x1f01ff != 0x1f01ff {
			return securityError(directory, "unexpected_acl", ErrUnsafe)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		matched := false
		for i, trustee := range identity.trustees {
			if windows.EqualSid(sid, trustee) {
				if seen[i] {
					return securityError(directory, "unexpected_acl", ErrUnsafe)
				}
				seen[i], matched = true, true
				break
			}
		}
		if !matched {
			return securityError(directory, "unexpected_acl", ErrUnsafe)
		}
	}
	return nil
}

// AppContainers require both the user and the specific container to be granted
// access. Do not substitute All Application Packages or a capability shared by
// unrelated containers. Ordinary processes retain the user/SYSTEM boundary.
func storageAppContainerSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	var app, size uint32
	if err := windows.GetTokenInformation(token, 29, (*byte)(unsafe.Pointer(&app)), uint32(unsafe.Sizeof(app)), &size); err != nil {
		return nil, err
	}
	if app == 0 {
		return nil, nil
	}
	// TokenAppContainerSid (31) returns TOKEN_APPCONTAINER_INFORMATION,
	// containing a SID pointer into the returned token information buffer.
	err := windows.GetTokenInformation(token, 31, nil, 0, &size)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		if err == nil {
			err = windows.ERROR_INVALID_DATA
		}
		return nil, err
	}
	if size < uint32(unsafe.Sizeof(uintptr(0))) {
		return nil, windows.ERROR_INVALID_DATA
	}
	buffer := make([]byte, size)
	if err := windows.GetTokenInformation(token, 31, &buffer[0], size, &size); err != nil {
		return nil, err
	}
	sid := *(**windows.SID)(unsafe.Pointer(&buffer[0]))
	if sid == nil || !sid.IsValid() {
		return nil, windows.ERROR_INVALID_DATA
	}
	copy, err := sid.Copy()
	runtime.KeepAlive(buffer)
	return copy, err
}
