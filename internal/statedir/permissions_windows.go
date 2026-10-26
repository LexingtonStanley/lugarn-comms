package statedir

import (
	"os"

	"golang.org/x/sys/windows"
)

func privateDACL() (*windows.ACL, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + u.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	return dacl, err
}

func protectDir(path string) error {
	dacl, err := privateDACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}

func protectFile(f *os.File) error {
	// os.CreateTemp's handle does not request WRITE_DAC. The named operation
	// opens an appropriate handle before any secret bytes are written.
	return protectDir(f.Name())
}
