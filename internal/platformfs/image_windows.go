package platformfs

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

// CheckTrustedImage applies to an executable held for an OS helper, not private
// storage. Local administrators/SYSTEM/TrustedInstaller are already privileged;
// other users must not be able to replace its image or parent path.
func CheckTrustedImage(file *File) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	installer, _, _, err := windows.LookupSID("", `NT SERVICE\TrustedInstaller`)
	if err != nil {
		return err
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && (windows.EqualSid(sid, user.User.Sid) || windows.EqualSid(sid, system) || windows.EqualSid(sid, admins) || windows.EqualSid(sid, installer))
	}
	check := func(handle windows.Handle, directory bool) error {
		sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			return err
		}
		owner, _, err := sd.Owner()
		if err != nil {
			return err
		}
		if !trusted(owner) {
			return ErrUnsafe
		}
		acl, _, err := sd.DACL()
		if err != nil {
			return err
		}
		if acl == nil {
			return ErrUnsafe
		}
		mask := uint32(windows.GENERIC_ALL | windows.GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.FILE_WRITE_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA)
		if directory {
			mask |= 0x40 /* FILE_DELETE_CHILD */
		} else {
			mask |= windows.FILE_APPEND_DATA
		}
		for index := uint32(0); index < uint32(acl.AceCount); index++ {
			var ace *windows.ACCESS_ALLOWED_ACE
			if err = windows.GetAce(acl, index, &ace); err != nil {
				return err
			}
			if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
				continue
			}
			if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
				return ErrUnsafe
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if uint32(ace.Mask)&mask != 0 && !trusted(sid) {
				return ErrUnsafe
			}
		}
		return nil
	}
	if err = check(windows.Handle(file.Fd()), false); err != nil {
		return err
	}
	for _, handle := range file.guard.handles {
		if err = check(handle, true); err != nil {
			return err
		}
	}
	return nil
}
