package main

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func ShowProcessStatsByPid(ctx context.Context, fcache *FileCache, pid int) error {
	cgroupPathBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return fmt.Errorf("failed to read cgroup path for pid %d: %v", pid, err)
	}
	// cgroupPathBytes example: "0::/system.slice/sshd.service"
	cgroupPath, ok := strings.CutPrefix(string(cgroupPathBytes), "0::")
	if !ok {
		return fmt.Errorf("unexpected cgroup path format for pid %d: %s", pid, cgroupPathBytes)
	}
	cgroupPath = strings.TrimSpace(cgroupPath)

	state1, err := CollectCgroupState(ctx, fcache, cgroupPath)
	if err != nil {
		return fmt.Errorf("failed to collect cgroup state1 for pid %d: %v", pid, err)
	}
	SleepWithLog(ctx, CFG.Interval)
	state2, err := CollectCgroupState(ctx, fcache, cgroupPath)
	if err != nil {
		return fmt.Errorf("failed to collect cgroup state2 for pid %d: %v", pid, err)
	}
	fmt.Printf("%d %5.2f%% %6.1fMiB %s\n",
		pid,
		float64(state2.Cpu-state1.Cpu)*100.0/float64(state2.Time.Sub(state1.Time).Microseconds()),
		float64(state2.Mem)/1024/1024,
		cgroupPath,
	)
	return nil
}

func ShowProcessStatsByCmd(ctx context.Context, fcache *FileCache, command string) error {
	pid, err := findPidByCmd(ctx, command)
	if err != nil {
		return fmt.Errorf("failed to find pid by command %s: %v", command, err)
	} else if pid == 0 {
		return fmt.Errorf("no process found for command %s", command)
	}
	return ShowProcessStatsByPid(ctx, fcache, pid)
}

const procDir = "/proc"

func findPidByCmd(ctx context.Context, command string) (int, error) {
	proc, err := os.Open(procDir)
	if err != nil {
		return 0, err
	}
	defer proc.Close()

	names, err := proc.Readdirnames(-1)
	if err != nil {
		return 0, err
	}

	var result string = "0"
	var oldest uint64 = math.MaxUint64
	for _, name := range names {
		if !isNumericPid(name) {
			continue
		}

		cmdline, err := os.ReadFile(filepath.Join(procDir, name, "cmdline"))
		if err != nil {
			LOG.Warnf(ctx, "failed to read cmdline for pid %s: %v", name, err)
			continue
		}
		if !bytes.Contains(cmdline, []byte(command)) {
			continue
		}

		if stat, err := os.ReadFile(filepath.Join(procDir, name, "stat")); err == nil {
			if fields := bytes.Fields(stat); len(fields) > 21 {
				starttime, err := strconv.ParseUint(string(fields[21]), 10, 64)
				if err == nil && starttime < oldest {
					oldest = starttime
					result = name
				}
			}
		}
		if result == "0" {
			result = name
		}
	}
	return strconv.Atoi(result)
}

func isNumericPid(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
