# cgstats
Collect and display cgroup statistics of a process or docker container at intervals.

## example
```sh
# List all docker containers stats after 30 seconds
go run ./cmd/cgstats -i 30

# Show cgroup stats for a specific process by pid
go run ./cmd/cgstats -p 1234

# Show cgroup stats for a specific process by cmdline substring
go run ./cmd/cgstats -c sshd
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
