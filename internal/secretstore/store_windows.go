//go:build windows

package secretstore

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = 1168
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type windowsStore struct{}

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func Open() (Store, error) { return windowsStore{}, nil }

func (windowsStore) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(target(key))
	if err != nil {
		return fmt.Errorf("encode credential target: %w", err)
	}
	user, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("encode credential user: %w", err)
	}
	blob := []byte(value)
	if len(blob) > 2560 {
		return fmt.Errorf("secret is too large for Windows Credential Manager")
	}
	var pointer *byte
	if len(blob) > 0 {
		pointer = &blob[0]
	}
	valueToWrite := credential{Type: credTypeGeneric, TargetName: name, CredentialBlobSize: uint32(len(blob)), CredentialBlob: pointer, Persist: credPersistLocalMachine, UserName: user}
	result, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&valueToWrite)), 0)
	if result == 0 {
		return fmt.Errorf("write Windows Credential Manager entry: %w", callErr)
	}
	return nil
}

func (windowsStore) Get(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	name, err := windows.UTF16PtrFromString(target(key))
	if err != nil {
		return "", err
	}
	var pointer *credential
	result, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&pointer)))
	if result == 0 {
		if errors.Is(callErr, windows.Errno(errorNotFound)) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read Windows Credential Manager entry: %w", callErr)
	}
	defer func() { _, _, _ = procCredFree.Call(uintptr(unsafe.Pointer(pointer))) }()
	if pointer.CredentialBlobSize == 0 || pointer.CredentialBlob == nil {
		return "", nil
	}
	return string(unsafe.Slice(pointer.CredentialBlob, int(pointer.CredentialBlobSize))), nil
}

func (windowsStore) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(target(key))
	if err != nil {
		return err
	}
	result, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0)
	if result == 0 {
		if errors.Is(callErr, windows.Errno(errorNotFound)) {
			return ErrNotFound
		}
		return fmt.Errorf("delete Windows Credential Manager entry: %w", callErr)
	}
	return nil
}
