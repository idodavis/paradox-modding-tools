package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWalk(t *testing.T) {
	dir := tree(t, map[string]string{
		"b/2.txt":      "",
		"a/1.txt":      "",
		"a/deep/0.txt": "",
		"top.txt":      "",
		".git/config":  "",
	})

	walked := func() []string {
		var got []string
		Walk(dir, func(p string) {
			rel, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(rel))
		})
		return got
	}

	// name order within a folder, folders entered where they sort, .git skipped
	want := []string{"a/1.txt", "a/deep/0.txt", "b/2.txt", "top.txt"}
	if got := walked(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk = %v; want %v", got, want)
	}

	// a link to a folder elsewhere is followed and spelled under the root; a link back to an ancestor is not entered
	outside := tree(t, map[string]string{"linked.txt": ""})
	link := func(target, at string) {
		if os.Symlink(target, at) == nil {
			return
		}

		// Windows refuses a symlink without a privilege, and a junction is what a linked mod folder usually is there
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", at, target).CombinedOutput(); err != nil {
			t.Skipf("this machine can make neither a symlink nor a junction: %v %s", err, out)
		}
	}
	link(outside, filepath.Join(dir, "c"))
	link(dir, filepath.Join(dir, "a", "loop"))
	want = []string{"a/1.txt", "a/deep/0.txt", "b/2.txt", "c/linked.txt", "top.txt"}
	if got := walked(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk with links = %v; want %v", got, want)
	}

	// a root that does not exist walks nothing
	Walk(filepath.Join(dir, "missing"), func(string) { t.Error("walked a missing root") })
}
