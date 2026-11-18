package platformfs

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// PrivateStamp identifies the file publication at path using checked metadata.
func PrivateStamp(path string) (string, error) {
	file, err := OpenPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%v/%v/%v/%v/%v", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow,
		info.CreationTime.Nanoseconds(), info.LastWriteTime.Nanoseconds()), nil
}
