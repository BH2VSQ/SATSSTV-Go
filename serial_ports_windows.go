//go:build windows

package main

import (
	"sort"

	"golang.org/x/sys/windows/registry"
)

// listSerialPorts enumerates the COM ports currently registered with Windows.
// It reads the SERIALCOMM device map, which holds one COMx value for every
// serial-capable device the system has enumerated. The ports are returned
// sorted for a stable UI order.
func listSerialPorts() []string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil
	}
	ports := make([]string, 0, len(names))
	for _, name := range names {
		if value, _, err := key.GetStringValue(name); err == nil && value != "" {
			ports = append(ports, value)
		}
	}
	sort.Strings(ports)
	return ports
}
