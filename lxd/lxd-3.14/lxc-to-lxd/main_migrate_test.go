package main

import (
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/lxc/lxd/lxd/types"
	"github.com/stretchr/testify/require"
	lxc "gopkg.in/lxc/go-lxc.v2"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     []string
		err        string
		shouldFail bool
	}{
		{
			"container migrated",
			[]string{
				"lxd.migrated = 1",
			},
			"Container has already been migrated",
			true,
		},
		{
			"container name missmatch (1)",
			[]string{
				"lxc.uts.name = c2",
			},
			"Container name doesn't match lxc.uts.name / lxc.utsname",
			true,
		},
		{
			"container name missmatch (2)",
			[]string{
				"lxc.utsname = c2",
			},
			"Container name doesn't match lxc.uts.name / lxc.utsname",
			true,
		},
		{
			"incomplete AppArmor support (1)",
			[]string{
				"lxc.uts.name = c1",
				"lxc.apparmor.allow_incomplete = 1",
			},
			"Container allows incomplete AppArmor support",
			true,
		},
		{
			"incomplete AppArmor support (2)",
			[]string{
				"lxc.uts.name = c1",
				"lxc.aa_allow_incomplete = 1",
			},
			"Container allows incomplete AppArmor support",
			true,
		},
		{
			"missing minimal /dev filesystem",
			[]string{
				"lxc.uts.name = c1",
				"lxc.apparmor.allow_incomplete = 0",
				"lxc.autodev = 0",
			},
			"Container doesn't mount a minimal /dev filesystem",
			true,
		},
		{
			"missing lxc.rootfs key",
			[]string{
				"lxc.uts.name = c1",
				"lxc.apparmor.allow_incomplete = 0",
				"lxc.autodev = 1",
			},
			"Invalid container, missing lxc.rootfs key",
			true,
		},
		{
			"non-existent rootfs path",
			[]string{
				"lxc.uts.name = c1",
				"lxc.apparmor.allow_incomplete = 0",
				"lxc.autodev = 1",
				"lxc.rootfs = dir:/invalid/path",
			},
			"Couldn't find the container rootfs '/invalid/path'",
			true,
		},
	}

	lxcPath, err := ioutil.TempDir("", "lxc-to-lxd-test-")
	require.NoError(t, err)
	defer os.RemoveAll(lxcPath)

	c, err := lxc.NewContainer("c1", lxcPath)
	require.NoError(t, err)

	for i, tt := range tests {
		log.Printf("Running test #%d: %s", i, tt.name)
		err := validateConfig(tt.config, c)
		if tt.shouldFail {
			require.EqualError(t, err, tt.err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestConvertNetworkConfig(t *testing.T) {
	tests := []struct {
		name            string
		config          []string
		expectedDevices types.Devices
		expectedError   string
		shouldFail      bool
	}{
		{
			"loopback only",
			[]string{},
			types.Devices{
				"eth0": map[string]string{
					"type": "none",
				},
			},
			"",
			false,
		},
		{
			"multiple network devices (sorted)",
			[]string{
				"lxc.net.0.type = macvlan",
				"lxc.net.0.macvlan.mode = bridge",
				"lxc.net.0.link = mvlan0",
				"lxc.net.0.hwaddr = 00:16:3e:8d:4f:51",
				"lxc.net.0.name = eth1",
				"lxc.net.1.type = veth",
				"lxc.net.1.link = lxcbr0",
				"lxc.net.1.hwaddr = 00:16:3e:a2:7d:54",
				"lxc.net.1.name = eth2",
			},
			types.Devices{
				"net1": map[string]string{
					"type":    "nic",
					"nictype": "bridged",
					"parent":  "lxcbr0",
					"name":    "eth2",
					"hwaddr":  "00:16:3e:a2:7d:54",
				},
				"eth0": map[string]string{
					"type": "none",
				},
				"net0": map[string]string{
					"name":    "eth1",
					"hwaddr":  "00:16:3e:8d:4f:51",
					"type":    "nic",
					"nictype": "macvlan",
					"parent":  "mvlan0",
				},
			},
			"",
			false,
		},
		{
			"multiple network devices (unsorted)",
			[]string{
				"lxc.net.0.type = macvlan",
				"lxc.net.1.name = eth2",
				"lxc.net.0.macvlan.mode = bridge",
				"lxc.net.0.link = mvlan0",
				"lxc.net.0.hwaddr = 00:16:3e:8d:4f:51",
				"lxc.net.0.name = eth1",
				"lxc.net.1.type = veth",
				"lxc.net.1.link = lxcbr0",
				"lxc.net.1.hwaddr = 00:16:3e:a2:7d:54",
			},
			types.Devices{
				"net1": map[string]string{
					"type":    "nic",
					"nictype": "bridged",
					"parent":  "lxcbr0",
					"name":    "eth2",
					"hwaddr":  "00:16:3e:a2:7d:54",
				},
				"eth0": map[string]string{
					"type": "none",
				},
				"net0": map[string]string{
					"name":    "eth1",
					"hwaddr":  "00:16:3e:8d:4f:51",
					"type":    "nic",
					"nictype": "macvlan",
					"parent":  "mvlan0",
				},
			},
			"",
			false,
		},
	}

	lxcPath, err := ioutil.TempDir("", "lxc-to-lxd-test-")
	require.NoError(t, err)
	defer os.RemoveAll(lxcPath)

	for i, tt := range tests {
		log.Printf("Running test #%d: %s", i, tt.name)

		c, err := lxc.NewContainer("c1", lxcPath)
		require.NoError(t, err)

		err = c.Create(lxc.TemplateOptions{Template: "busybox"})
		require.NoError(t, err)

		// In case the system uses a lxc.conf file
		c.ClearConfigItem("lxc.net.0")

		for _, conf := range tt.config {
			parts := strings.SplitN(conf, "=", 2)
			require.Equal(t, 2, len(parts))
			err := c.SetConfigItem(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			require.NoError(t, err)
		}

		devices := make(types.Devices, 0)
		err = convertNetworkConfig(c, devices)
		if tt.shouldFail {
			require.EqualError(t, err, tt.expectedError)
		} else {
			require.NoError(t, err)
			require.Equal(t, tt.expectedDevices, devices)
		}

		err = c.Destroy()
		require.NoError(t, err)
	}
}

func TestConvertStorageConfig(t *testing.T) {
	tests := []struct {
		name            string
		config          []string
		expectedDevices types.Devices
		expectedError   string
		shouldFail      bool
	}{
		{
			"invalid path",
			[]string{
				"lxc.mount.entry = /foo lib none ro,bind 0 0",
			},
			types.Devices{},
			"Invalid path: /foo",
			true,
		},
		{
			"ignored default mounts",
			[]string{
				"lxc.mount.entry = proc /proc proc defaults 0 0",
			},
			types.Devices{},
			"",
			false,
		},
		{
			"ignored mounts",
			[]string{
				"lxc.mount.entry = shm /dev/shm tmpfs defaults 0 0",
			},
			types.Devices{},
			"",
			false,
		},
		{
			"valid mount configuration",
			[]string{
				"lxc.rootfs.path = dir:/tmp",
				"lxc.mount.entry = /lib lib none ro,bind 0 0",
				"lxc.mount.entry = /usr/lib usr/lib none ro,bind 1 0",
				"lxc.mount.entry = /home home none ro,bind 0 0",
				"lxc.mount.entry = /sys/kernel/security /sys/kernel/security none ro,bind,optional 1 0",
				"lxc.mount.entry = /mnt /tmp/mnt none ro,bind 0 0",
			},
			types.Devices{
				"mount0": map[string]string{
					"type":     "disk",
					"readonly": "true",
					"source":   "/lib",
					"path":     "/lib",
				},
				"mount1": map[string]string{
					"type":     "disk",
					"readonly": "true",
					"source":   "/usr/lib",
					"path":     "/usr/lib",
				},
				"mount2": map[string]string{
					"type":     "disk",
					"readonly": "true",
					"source":   "/home",
					"path":     "/home",
				},
				"mount3": map[string]string{
					"type":     "disk",
					"readonly": "true",
					"optional": "true",
					"source":   "/sys/kernel/security",
					"path":     "/sys/kernel/security",
				},
				"mount4": map[string]string{
					"type":     "disk",
					"readonly": "true",
					"source":   "/mnt",
					"path":     "/mnt",
				},
			},
			"",
			false,
		},
	}

	for i, tt := range tests {
		log.Printf("Running test #%d: %s", i, tt.name)
		devices := make(types.Devices, 0)
		err := convertStorageConfig(tt.config, devices)
		if tt.shouldFail {
			require.EqualError(t, err, tt.expectedError)
		} else {
			require.NoError(t, err)
			require.Equal(t, tt.expectedDevices, devices)
		}
	}
}

func TestGetRootfs(t *testing.T) {
	tests := []struct {
		name           string
		config         []string
		expectedOutput string
		expectedError  string
		shouldFail     bool
	}{
		{
			"missing lxc.rootfs key",
			[]string{},
			"",
			"Invalid container, missing lxc.rootfs key",
			true,
		},
		{
			"valid lxc.rootfs key (1)",
			[]string{
				"lxc.rootfs = foobar",
			},
			"foobar",
			"",
			false,
		},
		{
			"valid lxc.rootfs key (2)",
			[]string{
				"lxc.rootfs = dir:foobar",
			},
			"foobar",
			"",
			false,
		},
	}

	for i, tt := range tests {
		log.Printf("Running test #%d: %s", i, tt.name)
		rootfs, err := getRootfs(tt.config)
		require.Equal(t, tt.expectedOutput, rootfs)
		if tt.shouldFail {
			require.EqualError(t, err, tt.expectedError)
		} else {
			require.NoError(t, err)
		}
	}
}
