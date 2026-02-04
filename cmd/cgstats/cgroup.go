package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const cgroupPrefix = "/sys/fs/cgroup"

var (
	reCpuUsageUsec    = regexp.MustCompile(`(?m)^usage_usec\s+([0-9]+)`)
	reMemCurrent      = regexp.MustCompile(`(?m)^([0-9]+)`)
	reMemInactiveFile = regexp.MustCompile(`(?m)^inactive_file\s+([0-9]+)`)
)

// https://docs.docker.com/engine/containers/runmetrics/
type CgroupState struct {
	Path string
	Time time.Time
	Cpu  int64
	Mem  int64
}

// https://github.com/containerd/cgroups/blob/31da8b0f7d670716395ff0c21f6d764f62bb7352/cgroup2/manager.go#L601
func (s *CgroupState) CollectCpuUsageUsec(fcache *FileCache) error {
	buf, err := fcache.SeekAndReadAll(filepath.Join(cgroupPrefix, s.Path, "cpu.stat"))
	if err != nil {
		return err
	}
	matched := reCpuUsageUsec.FindSubmatch(buf)
	if len(matched) < 2 {
		return fmt.Errorf("cpu_usage_usec not found")
	}
	s.Cpu, err = strconv.ParseInt(string(matched[1]), 10, 64)
	return err
}

// https://github.com/containerd/cgroups/blob/31da8b0f7d670716395ff0c21f6d764f62bb7352/cgroup2/manager.go#L622
// https://github.com/docker/cli/blob/769e75a0eeeadec99f3f9c6b8b9844fe7a9701e0/cli/command/container/stats_helpers.go#L238
// On cgroup v2 host, the result is `mem.Usage - mem.Stats["inactive_file"]`
func (s *CgroupState) CollectMemUsageBytes(fcache *FileCache) error {
	buf, err := fcache.SeekAndReadAll(filepath.Join(cgroupPrefix, s.Path, "memory.current"))
	if err != nil {
		return err
	}
	matched := reMemCurrent.FindSubmatch(buf)
	if len(matched) < 2 {
		return fmt.Errorf("mem_current not found")
	}
	s.Mem, err = strconv.ParseInt(string(matched[1]), 10, 64)
	if err != nil {
		return err
	}
	buf, err = fcache.SeekAndReadAll(filepath.Join(cgroupPrefix, s.Path, "memory.stat"))
	if err != nil {
		return err
	}
	matched = reMemInactiveFile.FindSubmatch(buf)
	if len(matched) < 2 {
		return fmt.Errorf("mem_inactive_file not found")
	}
	inactive, err := strconv.ParseInt(string(matched[1]), 10, 64)
	if err != nil {
		return err
	}
	s.Mem -= inactive
	return nil
}

// cgroupPath example: "/system.slice/sshd.service"
func CollectCgroupState(ctx context.Context, fcache *FileCache, cgroupPath string) (CgroupState, error) {
	state := CgroupState{
		Path: cgroupPath,
		Time: time.Now(),
	}
	if err := state.CollectCpuUsageUsec(fcache); err != nil {
		return state, err
	}
	if err := state.CollectMemUsageBytes(fcache); err != nil {
		return state, err
	}
	return state, nil
}

func SleepWithLog(ctx context.Context, seconds int) {
	var interval int
	if seconds <= 5 {
		interval = 1
	} else if seconds <= 20 {
		interval = 2
	} else {
		interval = seconds / 10
		if seconds%10 != 0 {
			interval++
		}
	}

	sum := 0
	LOG.Infof(ctx, "waiting %ds...", seconds)
	for ; sum+interval < seconds; sum += interval {
		time.Sleep(time.Duration(interval) * time.Second)
		LOG.Infof(ctx, "waiting %ds...", seconds-sum-interval)
	}
	time.Sleep(time.Duration(seconds-sum) * time.Second)
}
