package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ProductOfAmerica/cairn/internal/intent"
)

const fixtureRoot = "testdata/skill-tests/yaml-authoring"

type result struct {
	name string
	err  error
}

func main() {
	failures := 0
	checks := []func() result{
		checkAllYAMLParses,
		checkAllYAMLHasHeader,
		checkSourceHashValid,
		checkSourceHashDrift,
		checkStableProseByteIdentical,
		checkElicitationWriteback,
		checkValidationFailureNoLeakage,
		checkNoWhitespacePaths,
	}
	for _, c := range checks {
		r := c()
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", r.name, r.err)
			failures++
		} else {
			fmt.Fprintf(os.Stderr, "PASS %s\n", r.name)
		}
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func checkAllYAMLParses() result {
	var errs []string
	_ = filepath.Walk(fixtureRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return nil
		}
		body, _ := os.ReadFile(p)
		var v any
		if e := yaml.Unmarshal(body, &v); e != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, e))
		}
		return nil
	})
	return r("all-yaml-parses", errs)
}

func checkAllYAMLHasHeader() result {
	var errs []string
	_ = filepath.Walk(fixtureRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return nil
		}
		body, _ := os.ReadFile(p)
		if _, e := intent.ParseSourceHashHeader(body); e != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, e))
		}
		return nil
	})
	return r("all-yaml-has-header", errs)
}

func checkSourceHashValid() result {
	dir := filepath.Join(fixtureRoot, "source-hash-valid")
	prose := filepath.Join(dir, "design.md")
	derived := filepath.Join(dir, "derived.yaml")
	want, err := intent.ComputeSourceHash(prose)
	if err != nil {
		return r("source-hash-valid", []string{err.Error()})
	}
	body, err := os.ReadFile(derived)
	if err != nil {
		return r("source-hash-valid", []string{err.Error()})
	}
	h, err := intent.ParseSourceHashHeader(body)
	if err != nil {
		return r("source-hash-valid", []string{err.Error()})
	}
	if want != h.SourceHash {
		return r("source-hash-valid", []string{fmt.Sprintf("want %s, got %s", want, h.SourceHash)})
	}
	return r("source-hash-valid", nil)
}

func checkSourceHashDrift() result {
	dir := filepath.Join(fixtureRoot, "source-hash-drift")
	prose := filepath.Join(dir, "design.md")
	derived := filepath.Join(dir, "derived-stale.yaml")
	prosehash, err := intent.ComputeSourceHash(prose)
	if err != nil {
		return r("source-hash-drift", []string{err.Error()})
	}
	body, err := os.ReadFile(derived)
	if err != nil {
		return r("source-hash-drift", []string{err.Error()})
	}
	h, err := intent.ParseSourceHashHeader(body)
	if err != nil {
		if errors.Is(err, intent.ErrNoSourceHashHeader) {
			return r("source-hash-drift", []string{fmt.Sprintf("drift fixture %s missing header", derived)})
		}
		return r("source-hash-drift", []string{err.Error()})
	}
	if prosehash == h.SourceHash {
		return r("source-hash-drift", []string{"drift fixture has matching hash; should differ"})
	}
	return r("source-hash-drift", nil)
}

func checkStableProseByteIdentical() result {
	a, err := os.ReadFile(filepath.Join(fixtureRoot, "stable-prose", "regen-a.yaml"))
	if err != nil {
		return r("stable-prose-identical", []string{err.Error()})
	}
	b, err := os.ReadFile(filepath.Join(fixtureRoot, "stable-prose", "regen-b.yaml"))
	if err != nil {
		return r("stable-prose-identical", []string{err.Error()})
	}
	if string(a) != string(b) {
		return r("stable-prose-identical", []string{"regen-a.yaml and regen-b.yaml differ"})
	}
	return r("stable-prose-identical", nil)
}

func checkElicitationWriteback() result {
	body, err := os.ReadFile(filepath.Join(fixtureRoot, "elicitation-writeback", "design-after.md"))
	if err != nil {
		return r("elicitation-writeback", []string{err.Error()})
	}
	for _, want := range []string{"fixture/elicit/**", "go test -tags integration"} {
		if !strings.Contains(string(body), want) {
			return r("elicitation-writeback", []string{fmt.Sprintf("design-after.md missing %q", want)})
		}
	}
	return r("elicitation-writeback", nil)
}

func checkValidationFailureNoLeakage() result {
	body, err := os.ReadFile(filepath.Join(fixtureRoot, "validation-failure", "expected-design-question.txt"))
	if err != nil {
		return r("validation-failure-no-leakage", []string{err.Error()})
	}
	s := string(body)
	if strings.TrimSpace(s) == "" {
		return r("validation-failure-no-leakage", []string{"empty file"})
	}
	for _, banned := range []string{`"kind":`, `"code":`} {
		if strings.Contains(s, banned) {
			return r("validation-failure-no-leakage", []string{fmt.Sprintf("contains banned substring %q", banned)})
		}
	}
	return r("validation-failure-no-leakage", nil)
}

func checkNoWhitespacePaths() result {
	var bad []string
	_ = filepath.Walk("testdata/skill-tests", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.ContainsAny(p, " \t") {
			bad = append(bad, p)
		}
		return nil
	})
	return r("no-whitespace-paths", bad)
}

func r(name string, errs []string) result {
	if len(errs) == 0 {
		return result{name: name}
	}
	return result{name: name, err: fmt.Errorf("%s", strings.Join(errs, "; "))}
}
