package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/osutil"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

var CFG struct {
	Debug  bool   `flag:"d,false,Enable debug output"`
	Input  string `flag:"i,-,Kustomize build result as input, from file or stdin"`
	Output string `flag:"o,./output,Directory to save the split files in"`
	SubDir bool   `flag:"sub,false,Whether to create sub-directory for each kind"`
}

var LOG *logger.Logger

func setupConfigAndLogger(_ context.Context) {
	_, err := config.FromCommandLine(&CFG)
	if err != nil {
		panic(err)
	}
	level := logger.LevelInfo
	if CFG.Debug {
		level = logger.LevelDebug
	}
	LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{
		Level:     level,
		Colorful:  ansi.IsSupported(os.Stderr.Fd()),
		AddSource: CFG.Debug,
	}))
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	var dec kio.ByteReader
	if CFG.Input == "-" {
		dec = kio.ByteReader{Reader: os.Stdin, OmitReaderAnnotations: true}
	} else {
		fi, err := os.Open(CFG.Input)
		if err != nil {
			LOG.Fatalf(ctx, "failed to open input file %s: %v", CFG.Input, err)
		}
		defer fi.Close()
		dec = kio.ByteReader{Reader: fi, OmitReaderAnnotations: true}
	}

	nodes, err := dec.Read()
	if err != nil {
		LOG.Fatalf(ctx, "failed to read input: %v", err)
	} else if len(nodes) == 0 {
		LOG.Fatalf(ctx, "no resources found in input")
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return NodeIsLessThan(nodes[i], nodes[j])
	})

	idx := 1
	idxMap := make(map[string]int)
	for _, node := range nodes {
		kind := strings.ToLower(node.GetKind())
		if idxMap[kind] == 0 {
			idxMap[kind] = idx
			idx++
		}
		if err = writeNode(node, idxMap[kind]); err != nil {
			LOG.Fatalf(ctx, "failed to write resource %s/%s: %v", node.GetKind(), node.GetName(), err)
		}
	}
}

func fullPath(node *yaml.RNode, idx int) string {
	if CFG.SubDir {
		return filepath.Join(CFG.Output, fmt.Sprintf("%02d_%s", idx, strings.ToLower(node.GetKind())), node.GetName()+".yaml")
	}
	return filepath.Join(CFG.Output, fmt.Sprintf("%02d_%s_%s.yaml", idx, ShortName(node.GetKind()), strings.ToLower(node.GetName())))
}

func writeNode(node *yaml.RNode, idx int) error {
	if node.GetKind() == "" || node.GetName() == "" {
		return errors.New("missing kind or name for resource")
	}

	buf := &bytes.Buffer{}
	enc := yaml.NewEncoder(buf)
	if err := enc.Encode(node.Document()); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	fPath := fullPath(node, idx)
	if err := os.MkdirAll(filepath.Dir(fPath), osutil.DefaultDirMode); err != nil {
		return err
	}
	if _, err := os.Stat(fPath); !os.IsNotExist(err) {
		return errors.New("resource file already exists: " + fPath)
	}

	return os.WriteFile(fPath, buf.Bytes(), osutil.DefaultFileMode)
}
