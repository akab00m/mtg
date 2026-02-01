package network

import (
	"fmt"
	"os"
)

// logDNSError logs DNS resolution errors to stderr for debugging
func logDNSError(operation, hostname string, err error) {
	fmt.Fprintf(os.Stderr, "[DNS] %s failed for %s: %v\n", operation, hostname, err)
}
