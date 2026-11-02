package setup

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var javaVersionPattern = regexp.MustCompile(`(?i)(?:java|openjdk) version\s+"?([0-9]+)(?:\.([0-9]+))?`)

func CheckRequirements(javaExecutable, playerExecutable string) (int, error) {
	javaPath, err := exec.LookPath(javaExecutable)
	if err != nil {
		return 0, fmt.Errorf("Java executable %q not found; install a JRE 17 or newer: %w", javaExecutable, err)
	}
	output, err := exec.Command(javaPath, "-version").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("run %s -version: %w", javaExecutable, err)
	}
	major, err := ParseJavaMajor(string(output))
	if err != nil {
		return 0, err
	}
	if major < 17 {
		return major, fmt.Errorf("Java %d is too old; AnimiX requires a JRE 17 or newer", major)
	}
	if _, err := exec.LookPath(playerExecutable); err != nil {
		return major, fmt.Errorf("video player %q not found; install mpv or pass --player: %w", playerExecutable, err)
	}
	return major, nil
}

func ParseJavaMajor(output string) (int, error) {
	match := javaVersionPattern.FindStringSubmatch(output)
	if len(match) == 0 {
		return 0, fmt.Errorf("could not determine Java version from java -version output")
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("parse Java version: %w", err)
	}
	if major == 1 && len(match) > 2 && strings.TrimSpace(match[2]) != "" {
		major, err = strconv.Atoi(match[2])
		if err != nil {
			return 0, fmt.Errorf("parse legacy Java version: %w", err)
		}
	}
	return major, nil
}
