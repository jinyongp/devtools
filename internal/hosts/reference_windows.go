package hosts

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func referenceStamp(root string) string {
	file, err := platformfs.OpenPrivate(filepath.Join(root, "references.json"), os.O_RDONLY, 0)
	if err != nil {
		return ""
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) != nil {
		return ""
	}
	return fmt.Sprintf("%v/%v/%v/%v/%v", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow,
		info.CreationTime.Nanoseconds(), info.LastWriteTime.Nanoseconds())
}
