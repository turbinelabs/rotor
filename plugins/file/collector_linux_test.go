//+build linux

package file

import (
	"os"
	"testing"

	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/tempfile"
)

// Linux-only because it's flaky under OSX due to fsnotify bugs.
func TestFileCollectorRunUsingLinks(t *testing.T) {
	testFileCollectorRun(t,
		func(tempDir tempfile.Dir, file string) string {
			w := tempDir.Make(t, "link")
			assert.Nil(t, os.Remove(w))
			assert.Nil(t, os.Symlink(file, w))
			return w
		},
		func(file string, updatedFile string) {
			// Janky "atomic" ln -f (cool story, this is how coreutils does it):
			// create the link using a temporary file name and then rename it.
			random := file + ".xyzpdq"
			assertNilOrDie(t, os.Symlink(updatedFile, random))
			assertNilOrDie(t, os.Rename(random, file))
		},
	)
}
