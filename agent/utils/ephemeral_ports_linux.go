//go:build linux
// +build linux

// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package utils

import (
	"bufio"
	"fmt"
	"os"
)

const (
	// DefaultHostPortRangeStart indicates the first port in ephemeral host port range
	DefaultHostPortRangeStart = 49153
	// DefaultHostPortRangeEnd indicates the last port in ephemeral host port range
	DefaultHostPortRangeEnd = 65535
	// portRangeKernelParam is a kernel parameter that defines the ephemeral port range
	portRangeKernelParam = "/proc/sys/net/ipv4/ip_local_port_range"
)

// GetEphemeralHostPortRange returns the ephemeral port range defined by portRangeKernelParam
// Ref: https://github.com/moby/moby/blob/master/libnetwork/portallocator/portallocator_linux.go
func GetEphemeralHostPortRange() (start int, end int, err error) {
	file, err := os.Open(portRangeKernelParam)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	n, err := fmt.Fscanf(bufio.NewReader(file), "%d\t%d", &start, &end)
	if n != 2 || err != nil {
		if err == nil {
			err = fmt.Errorf("unexpected count of parsed numbers (%d)", n)
		}
		return DefaultHostPortRangeStart, DefaultHostPortRangeEnd, fmt.Errorf("failed to parse ephemeral port range from %s: %v",
			portRangeKernelParam, err)
	}
	return start, end, nil
}
