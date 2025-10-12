# ksplit
Split a Kustomize build output into individual, sorted resource files.

## example
```sh
# Read input from stdin
kustomize build overlays/prod | go run ./cmd/ksplit -o ./output

# Read input from file
go run ./cmd/ksplit -i ./final.yaml -o ./output

# Group by subdirectories per kind
go run ./cmd/ksplit -i ./final.yaml -o ./output -sub
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -i      string   Kustomize build result as input, from file or stdin [CFG_INPUT] (default "-")
  -o      string   Directory to save the split files in [CFG_OUTPUT] (default "./output")
  -sub    bool     Whether to create sub-directory for each kind [CFG_SUB_DIR]
```
