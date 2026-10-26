package statedir

import (
	"os"

	"golang.org/x/sys/windows"
)

// privateDACL is a protected DACL with one entry: full control for the current
// user. A directory's entry is inheritable (object and container inherit), so
// what's created inside gets the same single entry. That matters for what is
// already inside, too: setting a directory's DACL makes Windows re-propagate
// inheritance to its children, and a non-inheritable entry would strip their
// inherited entries and leave them openable by nobody (an open comms.db then
// refuses every new SQLite connection with "unable to open database file").
func privateDACL(inherit bool) (*windows.ACL, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	flags := ""
	if inherit {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;" + flags + ";FA;;;" + u.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	return dacl, err
}

func protectDir(path string) error { return protect(path, true) }

func protect(path string, inherit bool) error {
	dacl, err := privateDACL(inherit)
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
	return protect(f.Name(), false)
}
