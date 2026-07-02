package javatool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DetectTool inspects a project directory: "maven", "gradle" or "" (unknown).
func DetectTool(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pom.xml")); err == nil {
		return "maven"
	}
	for _, f := range []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return "gradle"
		}
	}
	return ""
}

// ResolveClasspath returns the service's compiled-classes + runtime-dependency
// classpath by asking the project's own build tool. No build-file changes are
// required in the service (PRD 1.3 amendment): Maven is interrogated via
// dependency:build-classpath, Gradle via an injected init script.
func ResolveClasspath(dir, tool string) (string, error) {
	if tool == "auto" || tool == "" {
		tool = DetectTool(dir)
	}
	switch tool {
	case "maven":
		return mavenClasspath(dir)
	case "gradle":
		return gradleClasspath(dir)
	case "none":
		return "", fmt.Errorf("--build-tool none requires an explicit --classpath")
	default:
		return "", fmt.Errorf("no pom.xml or build.gradle found in %s; pass --classpath explicitly", dir)
	}
}

func wrapper(dir, wrapperName, fallback string) string {
	p := filepath.Join(dir, wrapperName)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return fallback
}

func mavenClasspath(dir string) (string, error) {
	classes := filepath.Join(dir, "target", "classes")
	if _, err := os.Stat(classes); os.IsNotExist(err) {
		return "", fmt.Errorf("%s missing; compile first (mvn -DskipTests compile)", classes)
	}
	out, err := os.CreateTemp("", "wirefit-mvn-cp-*")
	if err != nil {
		return "", err
	}
	outPath := out.Name()
	out.Close()
	defer os.Remove(outPath)

	mvn := wrapper(dir, "mvnw", "mvn")
	cmd := exec.Command(mvn, "-q", "-DskipTests",
		"dependency:build-classpath",
		"-Dmdep.outputFile="+outPath,
		"-Dmdep.includeScope=runtime")
	cmd.Dir = dir
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("maven classpath resolution failed: %s: %w", firstLines(msg, 15), err)
	}
	depCP, err := os.ReadFile(outPath)
	if err != nil {
		return "", err
	}
	cp := classes
	if s := strings.TrimSpace(string(depCP)); s != "" {
		cp += string(os.PathListSeparator) + s
	}
	return cp, nil
}

// gradleInit registers a wirefitClasspath task on every java subproject; output
// lines are prefixed so -q noise from other plugins cannot corrupt parsing.
const gradleInit = `
allprojects {
    afterEvaluate { p ->
        if (p.plugins.hasPlugin('java')) {
            p.tasks.register('wirefitClasspath') {
                doLast {
                    println 'CT-CP:' + p.sourceSets.main.runtimeClasspath.asPath
                }
            }
        }
    }
}
`

func gradleClasspath(dir string) (string, error) {
	init, err := os.CreateTemp("", "wirefit-init-*.gradle")
	if err != nil {
		return "", err
	}
	initPath := init.Name()
	if _, err := init.WriteString(gradleInit); err != nil {
		init.Close()
		return "", err
	}
	init.Close()
	defer os.Remove(initPath)

	gradle := wrapper(dir, "gradlew", "gradle")
	cmd := exec.Command(gradle, "-q", "--console=plain", "--init-script", initPath, "wirefitClasspath")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gradle classpath resolution failed: %s: %w", firstLines(out, 15), err)
	}
	var parts []string
	for _, line := range strings.Split(string(out), "\n") {
		if cp, ok := strings.CutPrefix(strings.TrimSpace(line), "CT-CP:"); ok && cp != "" {
			parts = append(parts, cp)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("gradle produced no classpath (is the java plugin applied?)")
	}
	return strings.Join(parts, string(os.PathListSeparator)), nil
}

func firstLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
