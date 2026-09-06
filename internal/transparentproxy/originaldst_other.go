//go:build !linux

package transparentproxy

import (
	"fmt"
	"net"
)

func OriginalDestination(*net.TCPConn) (string, error) {
	return "", fmt.Errorf("SO_ORIGINAL_DST is only supported on Linux")
}
