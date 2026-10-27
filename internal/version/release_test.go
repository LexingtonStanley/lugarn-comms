package version

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The app's installer constructs release URLs and extracts these root files.
// Keep the workflow's tag, target, archive and checksum contract explicit.
func TestReleaseArchiveContract(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	require := func(t *testing.T, body, part string) {
		t.Helper()
		if !strings.Contains(body, part) {
			t.Errorf("release contract missing %q", part)
		}
	}
	t.Run("tag-version", func(t *testing.T) {
		require(t, workflow, `tags: ['v*']`)
		if !regexp.MustCompile(`(?m)^\s+VERSION: \$\{\{ github\.ref_name \}\}$`).MatchString(workflow) {
			t.Error("VERSION must preserve the tag including its v prefix")
		}
	})
	t.Run("six-targets", func(t *testing.T) {
		match := regexp.MustCompile(`for target in ([^;\n]+); do`).FindStringSubmatch(workflow)
		if match == nil {
			t.Fatal("no release target loop")
		}
		got := strings.Fields(match[1])
		sort.Strings(got)
		want := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("release targets = %v, want %v", got, want)
		}
		require(t, workflow, `target_os=${target%/*}`)
		require(t, workflow, `target_arch=${target#*/}`)
	})
	t.Run("archive-names", func(t *testing.T) {
		require(t, workflow, `package="lugarn-comms_${VERSION}_${target_os}_${target_arch}"`)
		require(t, workflow, `if [ "$target_os" = windows ]; then`)
		require(t, workflow, `zipfile.ZipFile(f"dist/{package}.zip", "w", compression=zipfile.ZIP_DEFLATED)`)
		require(t, workflow, `tar -czf "dist/$package.tar.gz"`)
	})
	t.Run("root-contents", func(t *testing.T) {
		require(t, workflow, `cp LICENSE.md NOTICE README.md "dist/$package/"`)
		require(t, workflow, `for name in ("lugarn-comms.exe", "LICENSE.md", "NOTICE", "README.md"):`)
		require(t, workflow, `archive.write(directory / name, arcname=name)`)
		require(t, workflow, `tar -czf "dist/$package.tar.gz" -C "dist/$package" lugarn-comms LICENSE.md NOTICE README.md`)
	})
	t.Run("published-checksums", func(t *testing.T) {
		require(t, workflow, `working-directory: dist`)
		require(t, workflow, `sha256sum -- *.tar.gz *.zip > SHA256SUMS`)
		_, publish, ok := strings.Cut(workflow, `gh release create "$VERSION"`)
		if !ok {
			t.Fatal("no tagged GitHub release publication")
		}
		require(t, publish, `dist/*.tar.gz dist/*.zip dist/SHA256SUMS`)
	})
}
