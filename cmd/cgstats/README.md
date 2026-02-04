# cgstats
Collect and display cgroup statistics of a process or docker containers.

## example
```sh
# List all docker containers stats after 30 seconds
go run ./cmd/cgstats -i 30
#  1  0.00%    3.3MiB qbittorrent-nginx-1
#  2  0.09%   21.1MiB qbittorrent-app-1

# Show cgroup stats for a specific process by pid
go run ./cmd/cgstats -p 14764
# 14764  0.83%   10.1MiB /user.slice/user-0.slice/session-7.scope

# Show cgroup stats for a specific process by cmdline substring
go run ./cmd/cgstats -c sshd
# 533  0.00%    8.8MiB /system.slice/sshd.service
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -i      int      Interval seconds between two measurements [CFG_INTERVAL] (default 5)
  -p      int      Find target process by pid [CFG_PID]
  -c      string   Find target process by cmdline substring, oldest if multiple matches [CFG_CMD]
  -s      string   List all docker containers by docker socket [CFG_SOCK] (default "/var/run/docker.sock")
```
