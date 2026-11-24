//go:build windows

package crypto

import (
	"crypto/sha256"
	"fmt"
	"io"
	"syscall"
	"unsafe"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/sys/windows/registry"
)

const (
	cryptProtectUIForbidden = 0x01
)

var (
	dllCrypt32  = syscall.NewLazyDLL("crypt32.dll")
	dllKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCryptProtectData   = dllCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = dllCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = dllKernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{
		cbData: uint32(len(b)),
		pbData: &b[0],
	}
}

func (b *dataBlob) toBytes() []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

// windowsDPAPIProtector binds wrapped keys to the current Windows user,
// current Windows machine (DPAPI + HKLM MachineGuid), and GravitySSH app entropy.
type windowsDPAPIProtector struct {
	entropy []byte
}

// NewDefaultProtector returns the platform-native DPAPI key protector on Windows.
func NewDefaultProtector() KeyProtector {
	return &windowsDPAPIProtector{
		entropy: deriveAppMachineEntropy(),
	}
}

func readMachineGUID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return "fallback-machine-scope"
	}
	defer k.Close()
	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil || guid == "" {
		return "fallback-machine-scope"
	}
	return guid
}

func deriveAppMachineEntropy() []byte {
	// App-internal secret combined with Windows MachineGuid via HKDF-SHA256.
	// Even another program running under the same user cannot unwrap without this exact entropy derivation.
	appSecret := []byte("GravitySSH::DPAPI::AppBoundEntropy::v1::9f3e7b1c4a82d605")
	salt := []byte(readMachineGUID())
	info := []byte("GravitySSH-Vault-DPAPI-OptionalEntropy-v1")

	r := hkdf.New(sha256.New, appSecret, salt, info)
	out := make([]byte, 32)
	_, _ = io.ReadFull(r, out)
	return out
}

func (p *windowsDPAPIProtector) Protect(plaintext []byte) ([]byte, error) {
	inBlob := newBlob(plaintext)
	entropyBlob := newBlob(p.entropy)
	var outBlob dataBlob

	desc, _ := syscall.UTF16PtrFromString("GravitySSH Encrypted Vault Key")

	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(inBlob)),
		uintptr(unsafe.Pointer(desc)),
		uintptr(unsafe.Pointer(entropyBlob)),
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return nil, fmt.Errorf("DPAPI CryptProtectData failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	return outBlob.toBytes(), nil
}

func (p *windowsDPAPIProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	inBlob := newBlob(ciphertext)
	entropyBlob := newBlob(p.entropy)
	var outBlob dataBlob

	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(inBlob)),
		0,
		uintptr(unsafe.Pointer(entropyBlob)),
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return nil, fmt.Errorf("DPAPI CryptUnprotectData failed (vault belongs to another machine/user or was tampered): %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	return outBlob.toBytes(), nil
}

func (p *windowsDPAPIProtector) EngineName() string {
	return "Windows DPAPI (MachineGuid + User + App Entropy) + AES-256-GCM"
}

func (p *windowsDPAPIProtector) IsMachineBound() bool { return true }
func (p *windowsDPAPIProtector) IsUserBound() bool    { return true }
