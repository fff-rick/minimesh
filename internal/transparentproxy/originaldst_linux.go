//go:build linux

package transparentproxy

import (
	"encoding/binary"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

const soOriginalDst = 80

// OriginalDestination reads the IPv4 destination saved by netfilter REDIRECT.
func OriginalDestination(connection *net.TCPConn) (string, error) {
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return "", err
	}
	var destination unix.RawSockaddrInet4
	var socketErr error
	controlErr := rawConnection.Control(func(fd uintptr) {
		size := uint32(unsafe.Sizeof(destination))
		_, _, errno := unix.Syscall6(
			unix.SYS_GETSOCKOPT,
			fd,
			unix.SOL_IP,
			soOriginalDst,
			uintptr(unsafe.Pointer(&destination)),
			uintptr(unsafe.Pointer(&size)),
			0,
		)
		if errno != 0 {
			socketErr = errno
		}
	})
	if controlErr != nil {
		return "", controlErr
	}
	if socketErr != nil {
		return "", socketErr
	}
	if destination.Family != unix.AF_INET {
		return "", fmt.Errorf("unsupported address family %d", destination.Family)
	}
	portBytes := (*[2]byte)(unsafe.Pointer(&destination.Port))
	port := binary.BigEndian.Uint16(portBytes[:])
	return (&net.TCPAddr{IP: net.IP(destination.Addr[:]), Port: int(port)}).String(), nil
}
