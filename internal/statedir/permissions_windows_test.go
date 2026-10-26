package statedir

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assertPrivate(t *testing.T, path string, dir bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("DACL inheritance is not protected")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl == nil || acl.AceCount != 1 {
		t.Fatalf("DACL must contain exactly one ACE: %v", acl)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !windows.EqualSid(sid, u.User.Sid) {
		t.Fatalf("ACE SID = %s; want %s", sid, u.User.Sid)
	}
	// FILE_ALL_ACCESS from winnt.h: STANDARD_RIGHTS_REQUIRED | SYNCHRONIZE | 0x1ff.
	const fileAllAccess = 0x001f01ff
	var flags uint8
	if dir {
		flags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != flags || ace.Mask != fileAllAccess {
		t.Fatalf("unexpected ACE: %+v", ace)
	}
}
