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
	Time time.Time
	Cpu  int64
	Mem  int64
}

// cgroupPath example: "/system.slice/sshd.service"
func CollectCgroupState(ctx context.Context, fcache *FileCache, cgroupPath string) (res CgroupState, err error) {
	res.Time = time.Now()

	// cpu
	buf, err := fcache.SeekAndReadAll(filepath.Join(cgroupPrefix, cgroupPath, "cpu.stat"))
	if err != nil {
		return res, err
	}
	matched := reCpuUsageUsec.FindSubmatch(buf)
	if len(matched) < 2 {
		return res, fmt.Errorf("cpu_usage_usec not found")
	}
	res.Cpu, err = strconv.ParseInt(string(matched[1]), 10, 64)
	if err != nil {
		return res, err
	}

	// mem
	buf, err = fcache.SeekAndReadAll(filepath.Join(cgroupPrefix, cgroupPath, "memory.current"))
	if err != nil {
		return res, err
	}
	matched = reMemCurrent.FindSubmatch(buf)
	if len(matched) < 2 {
		return res, fmt.Errorf("mem_current not found")
	}
	res.Mem, err = strconv.ParseInt(string(matched[1]), 10, 64)
	if err != nil {
		return res, err
	}
	buf, err = fcache.SeekAndReadAll(filepath.Join(cgroupPrefix, cgroupPath, "memory.stat"))
	if err != nil {
		return res, err
	}
	matched = reMemInactiveFile.FindSubmatch(buf)
	if len(matched) < 2 {
		return res, fmt.Errorf("mem_inactive_file not found")
	}
	inactive, err := strconv.ParseInt(string(matched[1]), 10, 64)
	if err != nil {
		return res, err
	}
	res.Mem -= inactive

	return res, err
}

func SleepWithLog(ctx context.Context, seconds int) {
	d := time.Duration(seconds) * time.Second
	LOG.Infof(ctx, "sleeping for %v...", d)
	time.Sleep(d)
}
