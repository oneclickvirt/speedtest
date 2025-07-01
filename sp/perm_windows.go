//go:build windows
// +build windows

package sp

import (
	"golang.org/x/sys/windows"
)

func hasRootPermission() bool {
	sid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}
