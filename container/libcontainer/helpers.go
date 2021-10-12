// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package libcontainer

import (
	"fmt"

	info "github.com/google/cadvisor/info/v1"

	"github.com/opencontainers/runc/libcontainer/cgroups"

	"github.com/google/cadvisor/container"

	fs "github.com/opencontainers/runc/libcontainer/cgroups/fs"
	fs2 "github.com/opencontainers/runc/libcontainer/cgroups/fs2"
	configs "github.com/opencontainers/runc/libcontainer/configs"
	"k8s.io/klog/v2"
)

// GetCgroupSubsystems returns information about the cgroup subsystems that are
// of interest as a map of cgroup controllers to their mount points.
// For example, "cpu" -> "/sys/fs/cgroup/cpu".
func GetCgroupSubsystems(includedMetrics container.MetricSet) (map[string]string, error) {
	// Get all cgroup mounts.
	allCgroups, err := cgroups.GetCgroupMounts(true)
	if err != nil {
		return nil, err
	}

	disableCgroups := map[string]struct{}{}

	if !includedMetrics.Has(container.DiskIOMetrics) {
		disableCgroups["blkio"] = struct{}{}
		disableCgroups["io"] = struct{}{}
	}

	if !includedMetrics.Has(container.CpuUsageMetrics) {
		disableCgroups["cpu"] = struct{}{}
	}

	if !includedMetrics.Has(container.CPUSetMetrics) {
		disableCgroups["cpuset"] = struct{}{}
	}

	if !includedMetrics.Has(container.HugetlbUsageMetrics) {
		disableCgroups["hugetlb"] = struct{}{}
	}

	if !includedMetrics.Has(container.MemoryUsageMetrics) {
		disableCgroups["memory"] = struct{}{}
	}

	if !includedMetrics.Has(container.PerfMetrics) {
		disableCgroups["perf_event"] = struct{}{}
	}

	if !includedMetrics.Has(container.ProcessMetrics) {
		disableCgroups["pids"] = struct{}{}
	}

	return getCgroupSubsystemsHelper(allCgroups, disableCgroups)
}

// Get information about all the cgroup subsystems.
func GetAllCgroupSubsystems() (map[string]string, error) {
	// Get all cgroup mounts.
	allCgroups, err := cgroups.GetCgroupMounts(true)
	if err != nil {
		return nil, err
	}

	emptyDisableCgroups := map[string]struct{}{}
	return getCgroupSubsystemsHelper(allCgroups, emptyDisableCgroups)
}

func getCgroupSubsystemsHelper(allCgroups []cgroups.Mount, disableCgroups map[string]struct{}) (map[string]string, error) {
	if len(allCgroups) == 0 {
		return nil, fmt.Errorf("failed to find cgroup mounts")
	}

	// Trim the mounts to only the subsystems we care about.
	mountPoints := make(map[string]string, len(allCgroups))
	for _, mount := range allCgroups {
		for _, subsystem := range mount.Subsystems {
			if _, exists := disableCgroups[subsystem]; exists {
				continue
			}
			if _, ok := supportedSubsystems[subsystem]; !ok {
				// Unsupported subsystem
				continue
			}
			if _, ok := mountPoints[subsystem]; ok {
				// duplicate mount for this subsystem; use the first one we saw
				klog.V(5).Infof("skipping %s, already using mount at %s", mount.Mountpoint, mountPoints[subsystem])
				continue
			}
			mountPoints[subsystem] = mount.Mountpoint
		}
	}

	return mountPoints, nil
}

// Cgroup subsystems we support listing (should be the minimal set we need stats from).
var supportedSubsystems map[string]struct{} = map[string]struct{}{
	"cpu":        {},
	"cpuacct":    {},
	"memory":     {},
	"hugetlb":    {},
	"pids":       {},
	"cpuset":     {},
	"blkio":      {},
	"io":         {},
	"devices":    {},
	"perf_event": {},
}

func diskStatsCopy0(major, minor uint64) *info.PerDiskStats {
	disk := info.PerDiskStats{
		Major: major,
		Minor: minor,
	}
	disk.Stats = make(map[string]uint64)
	return &disk
}

type diskKey struct {
	Major uint64
	Minor uint64
}

func diskStatsCopy1(diskStat map[diskKey]*info.PerDiskStats) []info.PerDiskStats {
	i := 0
	stat := make([]info.PerDiskStats, len(diskStat))
	for _, disk := range diskStat {
		stat[i] = *disk
		i++
	}
	return stat
}

func diskStatsCopy(blkioStats []cgroups.BlkioStatEntry) (stat []info.PerDiskStats) {
	if len(blkioStats) == 0 {
		return
	}
	diskStat := make(map[diskKey]*info.PerDiskStats)
	for i := range blkioStats {
		major := blkioStats[i].Major
		minor := blkioStats[i].Minor
		key := diskKey{
			Major: major,
			Minor: minor,
		}
		diskp, ok := diskStat[key]
		if !ok {
			diskp = diskStatsCopy0(major, minor)
			diskStat[key] = diskp
		}
		op := blkioStats[i].Op
		if op == "" {
			op = "Count"
		}
		diskp.Stats[op] = blkioStats[i].Value
	}
	return diskStatsCopy1(diskStat)
}

func NewCgroupManager(name string, paths map[string]string) (cgroups.Manager, error) {
	if cgroups.IsCgroup2UnifiedMode() {
		path := paths["cpu"]
		return fs2.NewManager(nil, path, false)
	}

	config := configs.Cgroup{
		Name: name,
	}
	return fs.NewManager(&config, paths, false), nil
}
