package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

func ShowDockerStatsBySock(ctx context.Context, fcache *FileCache, dockerSock string) error {
	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", dockerSock)
			},
		},
	}

	// https://docs.docker.com/reference/api/engine/#api-version-matrix
	// https://docs.docker.com/reference/api/engine/version/v1.53/#tag/Container/operation/ContainerList
	resp, err := client.Get("http://docker/containers/json")
	if err != nil {
		return fmt.Errorf("failed to get docker containers: %v", err)
	}
	defer resp.Body.Close()

	var containers []struct {
		Id    string
		Names []string
		state CgroupState // ignored in JSON decode
	}
	if err = json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return fmt.Errorf("failed to decode docker containers: %v", err)
	} else if len(containers) == 0 {
		LOG.Warn(ctx, "no docker containers found")
		return nil
	}

	for i, c := range containers {
		// https://docs.docker.com/engine/containers/runmetrics/
		// /sys/fs/cgroup/system.slice/docker-<longid>.scope/ on cgroup v2, systemd driver
		containers[i].state, err = CollectCgroupState(ctx, fcache, fmt.Sprintf("/system.slice/docker-%s.scope", c.Id))
		if err != nil {
			return fmt.Errorf("failed to collect cgroup state1 for container %s(%s): %v", c.Names[0][1:], c.Id, err)
		}
	}
	SleepWithLog(ctx, CFG.Interval)
	for i, c := range containers {
		state2, err := CollectCgroupState(ctx, fcache, fmt.Sprintf("/system.slice/docker-%s.scope", c.Id))
		if err != nil {
			return fmt.Errorf("failed to collect cgroup state2 for container %s(%s): %v", c.Names[0][1:], c.Id, err)
		}
		fmt.Printf("%2d %5.2f%% %6.1fMiB %-s\n",
			i+1,
			float64(state2.Cpu-c.state.Cpu)*100.0/float64(state2.Time.Sub(c.state.Time).Microseconds()),
			float64(state2.Mem)/1024/1024,
			c.Names[0][1:],
		)
	}
	return nil
}
