// wirefit-java: the official Java extractor, an external executable
// speaking the public protocol (docs/extractor-protocol.md). Java FQNs have
// no file suffix, so route to it per manifest with the wildcard fallback:
// extractors: [{match: "*", command: "wirefit-java"}]. Configuration rides
// on the command itself, e.g. "wirefit-java --build-tool gradle".
package main

import (
	"encoding/json"
	"flag"
	"os"
	"sort"

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/extserve"
	"github.com/wirefit/wirefit/internal/javatool"
)

func main() {
	fs := flag.NewFlagSet("wirefit-java", flag.ContinueOnError)
	classpath := fs.String("classpath", "", "service classpath override (skips build-tool resolution)")
	buildTool := fs.String("build-tool", "auto", "auto|maven|gradle|none (how to resolve the service classpath)")
	extractorCP := fs.String("extractor-cp", os.Getenv("WIREFIT_EXTRACTOR_CP"),
		"override for WirefitExtract+jackson classpath (default: self-bootstrapped cache)")
	mapper := fs.String("mapper", "", "ObjectMapper provider <class-fqn>#<static-method>")
	javaBin := fs.String("java", "java", "java binary")
	if fs.Parse(os.Args[1:]) != nil {
		os.Exit(2)
	}
	opts := javatool.RunOptions{
		Classpath:   *classpath,
		BuildTool:   *buildTool,
		ExtractorCP: *extractorCP,
		Mapper:      *mapper,
		JavaBin:     *javaBin,
	}
	// Jackson draws no input/output distinction, so roles are ignored and a
	// ref used on both sides extracts once.
	os.Exit(extserve.Serve(func(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
		opts.ProjectDir = projectDir
		return javatool.Run(opts, refs(specs))
	}))
}

func refs(specs []extproto.Spec) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		if !seen[s.Ref] {
			seen[s.Ref] = true
			out = append(out, s.Ref)
		}
	}
	sort.Strings(out)
	return out
}
