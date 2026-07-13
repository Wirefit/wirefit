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

	"github.com/wirefit/wirefit/internal/extproto"
	"github.com/wirefit/wirefit/internal/extserve"
	"github.com/wirefit/wirefit/internal/javatool"
)

func main() {
	opts, code := parse(os.Args[1:])
	if code != 0 {
		os.Exit(code)
	}
	os.Exit(extserve.Serve(func(projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
		return extract(opts, projectDir, specs)
	}))
}

func parse(args []string) (javatool.RunOptions, int) {
	fs := flag.NewFlagSet("wirefit-java", flag.ContinueOnError)
	classpath := fs.String("classpath", "", "service classpath override (skips build-tool resolution)")
	buildTool := fs.String("build-tool", "auto", "auto|maven|gradle|none (how to resolve the service classpath)")
	extractorCP := fs.String("extractor-cp", os.Getenv("WIREFIT_EXTRACTOR_CP"),
		"override for WirefitExtract+jackson classpath (default: self-bootstrapped cache)")
	mapper := fs.String("mapper", "", "ObjectMapper provider <class-fqn>#<static-method>")
	javaBin := fs.String("java", "java", "java binary")
	if fs.Parse(args) != nil {
		return javatool.RunOptions{}, 2
	}
	return javatool.RunOptions{
		Classpath:   *classpath,
		BuildTool:   *buildTool,
		ExtractorCP: *extractorCP,
		Mapper:      *mapper,
		JavaBin:     *javaBin,
	}, 0
}

var runJava = javatool.Run

// Jackson draws no input/output distinction, so roles are ignored and a ref
// used on both sides extracts once.
func extract(opts javatool.RunOptions, projectDir string, specs []extproto.Spec) (map[string]json.RawMessage, error) {
	opts.ProjectDir = projectDir
	return runJava(opts, extserve.Refs(specs))
}
